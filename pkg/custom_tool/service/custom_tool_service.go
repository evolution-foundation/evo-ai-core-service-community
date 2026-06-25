package service

import (
	"bytes"
	"context"
	"encoding/json"
	errorsPostgres "evo-ai-core-service/internal/infra/postgres"
	"evo-ai-core-service/internal/utils/stringutils"
	model "evo-ai-core-service/pkg/custom_tool/model"
	repository "evo-ai-core-service/pkg/custom_tool/repository"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
)

// SSRF defenses for runToolTest:
//
//  1. Scheme allowlist (http / https).
//  2. Host validation pre-dial AND a custom DialContext that re-validates
//     the resolved IP at connect time — prevents DNS rebinding where the
//     name resolves to a public IP on first lookup and a private IP on
//     the actual dial.
//  3. Redirects disabled (server-side fetcher won't bounce into intranet).
//  4. Response body capped at responseBodyLimit.
//  5. Response headers passed back only via an allowlist (no Set-Cookie,
//     Authorization echo, etc.).
const (
	responseBodyLimit       int64 = 1 << 20 // 1 MiB
	clientTimeout                 = 15 * time.Second
	responseHeaderTimeout         = 10 * time.Second
	dialTimeout                   = 5 * time.Second
)

// responseHeaderAllowlist is the set of response headers safe to surface
// back to the user verbatim. Anything outside is dropped to avoid leaking
// downstream auth state (Set-Cookie, WWW-Authenticate, Proxy-*).
var responseHeaderAllowlist = map[string]struct{}{
	"Content-Type":     {},
	"Content-Length":   {},
	"Content-Encoding": {},
	"Date":             {},
	"Server":           {},
	"Cache-Control":    {},
	"Etag":             {},
	"Last-Modified":    {},
	"Location":         {}, // surface so the user sees the redirect target
}

var errBlockedHost = errors.New("URL aponta para endereço interno/privado; apenas hosts públicos são permitidos")

// allowLoopbackForTests, when true, lets validateEndpoint and safeDialContext
// accept 127.0.0.1/::1 so httptest.NewServer-backed tests can run. It MUST
// stay false in production. The SSRF unit tests deliberately leave it false
// when asserting that private hosts are rejected.
var allowLoopbackForTests = false

// validateEndpoint parses the user-supplied endpoint, enforces the scheme
// allowlist, and rejects hosts whose resolved IPs include any non-public
// address. Returns the parsed URL on success.
func validateEndpoint(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("URL inválida: %v", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("scheme não permitido: %q (apenas http/https)", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return nil, errors.New("URL sem host")
	}
	if ip := net.ParseIP(host); ip != nil {
		if !isPublicIP(ip) {
			return nil, errBlockedHost
		}
		return u, nil
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, fmt.Errorf("resolução DNS falhou: %v", err)
	}
	for _, ip := range ips {
		if !isPublicIP(ip) {
			return nil, errBlockedHost
		}
	}
	return u, nil
}

// isPublicIP returns true only for routable, public IP addresses.
// Rejects loopback, link-local, private (RFC1918), unspecified, multicast,
// IPv6 ULA (fc00::/7), and other non-public ranges.
func isPublicIP(ip net.IP) bool {
	if allowLoopbackForTests && ip.IsLoopback() {
		return true
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsPrivate() || ip.IsUnspecified() || ip.IsMulticast() || ip.IsInterfaceLocalMulticast() {
		return false
	}
	// Reject IPv4-mapped variants of the above.
	if v4 := ip.To4(); v4 != nil {
		if v4[0] == 0 || (v4[0] == 169 && v4[1] == 254) {
			return false
		}
	}
	return true
}

// safeDialContext re-validates the dialed IP on every connection attempt.
// This closes the DNS-rebinding window where validateEndpoint sees a
// public IP but the actual dial resolves to an internal one.
func safeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	for _, ip := range ips {
		if !isPublicIP(ip.IP) {
			return nil, errBlockedHost
		}
	}
	dialer := &net.Dialer{Timeout: dialTimeout}
	// Try each IP until one works; all already pass the public-IP gate.
	var lastErr error
	for _, ip := range ips {
		conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
		if dialErr == nil {
			return conn, nil
		}
		lastErr = dialErr
	}
	if lastErr == nil {
		lastErr = syscall.ECONNREFUSED
	}
	return nil, lastErr
}

// testHTTPClient is the dedicated client for the custom-tools test endpoint.
// Must NOT use the project's typed JSON helpers — custom tools can target
// arbitrary endpoints returning HTML, plain text, XML, etc.
var testHTTPClient = &http.Client{
	Timeout: clientTimeout,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		// Surface the redirect target instead of following it; following
		// would re-open the SSRF window (validated origin → arbitrary host).
		return http.ErrUseLastResponse
	},
	Transport: &http.Transport{
		DialContext:           safeDialContext,
		ResponseHeaderTimeout: responseHeaderTimeout,
		DisableKeepAlives:     true,
	},
}

// runToolTest issues the HTTP request described by the tool and returns a
// TestResult reflecting what actually happened: real status code, response
// time, headers, body. Success = HTTP 2xx (not body parseability).
func runToolTest(
	ctx context.Context,
	method, endpoint string,
	headers map[string]string,
	bodyParams map[string]interface{},
) *model.TestResult {
	result := &model.TestResult{}

	parsedURL, err := validateEndpoint(endpoint)
	if err != nil {
		result.Error = err.Error()
		return result
	}

	method = strings.ToUpper(method)
	var bodyReader io.Reader
	hasBody := len(bodyParams) > 0 && (method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch)
	if hasBody {
		buf, marshalErr := json.Marshal(bodyParams)
		if marshalErr != nil {
			result.Error = fmt.Sprintf("falha ao serializar body: %v", marshalErr)
			return result
		}
		bodyReader = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, parsedURL.String(), bodyReader)
	if err != nil {
		result.Error = fmt.Sprintf("falha ao construir request: %v", err)
		return result
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if hasBody && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	start := time.Now()
	resp, err := testHTTPClient.Do(req)
	elapsed := time.Since(start)
	result.ResponseTime = elapsed.Seconds()

	if err != nil {
		if errors.Is(err, errBlockedHost) || strings.Contains(err.Error(), errBlockedHost.Error()) {
			result.Error = errBlockedHost.Error()
			return result
		}
		msg := err.Error()
		switch {
		case strings.Contains(msg, "context deadline exceeded"), strings.Contains(msg, "Timeout"):
			result.Error = "Request timeout"
		case strings.Contains(msg, "no such host"):
			result.Error = "DNS resolution failed"
		case strings.Contains(msg, "connection refused"):
			result.Error = "Connection refused"
		default:
			result.Error = msg
		}
		return result
	}
	defer resp.Body.Close()

	limited := io.LimitReader(resp.Body, responseBodyLimit+1)
	body, readErr := io.ReadAll(limited)
	if readErr != nil {
		result.Error = fmt.Sprintf("falha ao ler body: %v", readErr)
		result.StatusCode = resp.StatusCode
		return result
	}
	truncated := int64(len(body)) > responseBodyLimit
	if truncated {
		body = body[:responseBodyLimit]
	}

	result.StatusCode = resp.StatusCode
	result.Body = string(body)
	result.Headers = filterResponseHeaders(resp.Header)
	if truncated {
		result.Headers["X-Evo-Body-Truncated"] = fmt.Sprintf("body truncated to %d bytes", responseBodyLimit)
	}
	result.Success = resp.StatusCode >= 200 && resp.StatusCode < 300
	if !result.Success {
		result.Error = fmt.Sprintf("HTTP %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	return result
}

// filterResponseHeaders applies the response header allowlist so we never
// echo Set-Cookie / Authorization / Proxy-* back to the caller.
func filterResponseHeaders(h http.Header) map[string]string {
	out := make(map[string]string, len(h))
	for k, v := range h {
		if _, ok := responseHeaderAllowlist[http.CanonicalHeaderKey(k)]; !ok {
			continue
		}
		if len(v) > 0 {
			out[http.CanonicalHeaderKey(k)] = v[0]
		}
	}
	return out
}

type CustomToolService interface {
	Create(ctx context.Context, request model.CustomTool) (*model.CustomTool, error)
	GetByID(ctx context.Context, id uuid.UUID) (*model.CustomTool, error)
	List(ctx context.Context, request model.CustomToolListRequest) (*model.CustomToolListResponse, error)
	Update(ctx context.Context, request *model.CustomTool, id uuid.UUID) (*model.CustomTool, error)
	Delete(ctx context.Context, id uuid.UUID) (bool, error)
	ConvertToHTTPTool(tool model.CustomToolResponse) map[string]interface{}
	Test(ctx context.Context, id uuid.UUID) (*model.CustomToolTestResponse, error)
}

type customToolService struct {
	customToolRepository repository.CustomToolRepository
}

func NewCustomToolService(customToolRepository repository.CustomToolRepository) CustomToolService {
	return &customToolService{
		customToolRepository: customToolRepository,
	}
}

func (s *customToolService) Create(ctx context.Context, request model.CustomTool) (*model.CustomTool, error) {
	customTool, err := s.customToolRepository.Create(ctx, request)

	if err != nil {
		return nil, errorsPostgres.MapDBError(err, model.CustomToolErrors)
	}

	return customTool, nil
}

func (s *customToolService) GetByID(ctx context.Context, id uuid.UUID) (*model.CustomTool, error) {
	customTool, err := s.customToolRepository.GetByID(ctx, id)

	if err != nil {
		return nil, errorsPostgres.MapDBError(err, model.CustomToolErrors)
	}

	return customTool, nil
}

func (s *customToolService) List(ctx context.Context, request model.CustomToolListRequest) (*model.CustomToolListResponse, error) {
	// Get paginated items
	customTools, err := s.customToolRepository.List(ctx, request)
	if err != nil {
		return nil, errorsPostgres.MapDBError(err, model.CustomToolErrors)
	}

	// Get total count
	totalItems, err := s.customToolRepository.Count(ctx, request)
	if err != nil {
		return nil, errorsPostgres.MapDBError(err, model.CustomToolErrors)
	}

	// Convert to response items
	items := make([]model.CustomToolResponse, len(customTools))
	for i, customTool := range customTools {
		items[i] = *customTool.ToResponse()
	}

	// Calculate pagination metadata
	totalPages := int((totalItems + int64(request.PageSize) - 1) / int64(request.PageSize))
	skip := (request.Page - 1) * request.PageSize
	limit := request.PageSize

	return &model.CustomToolListResponse{
		Items:      items,
		Page:       request.Page,
		PageSize:   request.PageSize,
		Skip:       skip,
		Limit:      limit,
		TotalItems: totalItems,
		TotalPages: totalPages,
	}, nil
}

func (s *customToolService) Update(ctx context.Context, request *model.CustomTool, id uuid.UUID) (*model.CustomTool, error) {
	_, err := s.GetByID(ctx, id)

	if err != nil {
		return nil, err
	}

	customTool, err := s.customToolRepository.Update(ctx, request, id)

	if err != nil {
		return nil, errorsPostgres.MapDBError(err, model.CustomToolErrors)
	}

	return customTool, nil
}

func (s *customToolService) Delete(ctx context.Context, id uuid.UUID) (bool, error) {
	_, err := s.GetByID(ctx, id)

	if err != nil {
		return false, err
	}

	deleted, err := s.customToolRepository.Delete(ctx, id)

	if err != nil {
		return false, errorsPostgres.MapDBError(err, model.CustomToolErrors)
	}

	return deleted, nil
}

func (s *customToolService) ConvertToHTTPTool(tool model.CustomToolResponse) map[string]interface{} {
	var errorHandling map[string]interface{}
	if tool.ErrorHandling != nil {
		errorHandling = tool.ErrorHandling
	}

	if _, ok := errorHandling["timeout"]; !ok {
		errorHandling["timeout"] = 30
	}
	if _, ok := errorHandling["retry_count"]; !ok {
		errorHandling["retry_count"] = 0
	}
	if _, ok := errorHandling["fallback_response"]; !ok {
		errorHandling["fallback_response"] = map[string]string{
			"error":   "",
			"message": "",
		}
	}

	return map[string]interface{}{
		"name":     tool.Name,
		"method":   tool.Method,
		"endpoint": tool.Endpoint,
		"headers":  tool.Headers,
		"parameters": map[string]interface{}{
			"path_params":  tool.PathParams,
			"query_params": tool.QueryParams,
			"body_params":  tool.BodyParams,
		},
		"description":    tool.Description,
		"error_handling": errorHandling,
		"values":         tool.Values,
	}
}

func (s *customToolService) Test(ctx context.Context, id uuid.UUID) (*model.CustomToolTestResponse, error) {
	customTool, err := s.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	response := customTool.ToResponse()
	method := strings.ToUpper(customTool.Method)

	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodHead, http.MethodOptions:
		// supported
	default:
		return nil, fmt.Errorf("unsupported method: %s", customTool.Method)
	}

	headers := stringutils.JSONToStringMap(customTool.Headers)
	bodyParams := stringutils.JSONToInterfaceMap(customTool.BodyParams)
	testResult := runToolTest(ctx, method, customTool.Endpoint, headers, bodyParams)

	return &model.CustomToolTestResponse{
		Tool:       response,
		TestResult: testResult,
	}, nil
}
