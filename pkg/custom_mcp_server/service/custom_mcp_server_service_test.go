package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"evo-ai-core-service/internal/config"
	"evo-ai-core-service/pkg/custom_mcp_server/model"
	"evo-ai-core-service/pkg/evoextensions/runtimecontext"

	"github.com/google/uuid"
)

// captureServer is a minimal httptest handler that records the inbound
// request headers so the test can assert what the service propagated.
type captureServer struct {
	*httptest.Server
	gotHeaders http.Header
}

func newCaptureServer(t *testing.T) *captureServer {
	t.Helper()
	cs := &captureServer{}
	cs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cs.gotHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"tools":[]}`))
	}))
	t.Cleanup(cs.Close)
	return cs
}

func newServiceForTest(url string) *customMcpServerService {
	return &customMcpServerService{
		customMcpServerRepository: nil,
		cfgAIProcessorService: &config.AIProcessorServiceConfig{
			URL:     url,
			Version: "v1",
		},
	}
}

// EVO-1623 (GO-4): when an enterprise scope binds a tenant id on the
// context, discoverTools must propagate it via X-Evo-Tenant-Id so the
// processor's runtime_context middleware (PY-1) can authorize the call.
func TestDiscoverTools_PropagatesTenantHeader_WhenBound(t *testing.T) {
	cs := newCaptureServer(t)
	svc := newServiceForTest(cs.URL)

	tenantID := uuid.NewString()
	ctx := context.WithValue(context.Background(), "token", "tok-abc")
	ctx = runtimecontext.WithID(ctx, tenantID)

	if _, err := svc.discoverTools(ctx, model.CustomMcpServer{URL: "https://example.test", Headers: "{}"}); err != nil {
		t.Fatalf("discoverTools: %v", err)
	}
	if got := cs.gotHeaders.Get("X-Evo-Tenant-Id"); got != tenantID {
		t.Fatalf("X-Evo-Tenant-Id: got %q want %q", got, tenantID)
	}
	if auth := cs.gotHeaders.Get("Authorization"); auth != "Bearer tok-abc" {
		t.Fatalf("Authorization: got %q", auth)
	}
}

// Community standalone build (and any request without a bound scope):
// the runtime context returns "" and the header MUST be omitted —
// this is the documented "no tenant → omit header" AC of EVO-1623.
func TestDiscoverTools_OmitsTenantHeader_WhenUnbound(t *testing.T) {
	cs := newCaptureServer(t)
	svc := newServiceForTest(cs.URL)

	ctx := context.WithValue(context.Background(), "token", "tok-xyz")

	if _, err := svc.discoverTools(ctx, model.CustomMcpServer{URL: "https://example.test", Headers: "{}"}); err != nil {
		t.Fatalf("discoverTools: %v", err)
	}
	if _, present := cs.gotHeaders["X-Evo-Tenant-Id"]; present {
		t.Fatalf("X-Evo-Tenant-Id leaked when no tenant bound: %q", cs.gotHeaders.Get("X-Evo-Tenant-Id"))
	}
}

// captureTestServer records inbound path+headers+body and returns a
// scriptable JSON envelope shaped like the processor's /test-connection
// response, so testConnection() can be exercised without a live processor.
type captureTestServer struct {
	*httptest.Server
	gotHeaders http.Header
	gotBody    []byte
	gotPath    string
}

func newCaptureTestServer(t *testing.T, responseJSON string) *captureTestServer {
	t.Helper()
	cs := &captureTestServer{}
	cs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cs.gotHeaders = r.Header.Clone()
		cs.gotPath = r.URL.Path
		cs.gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(responseJSON))
	}))
	t.Cleanup(cs.Close)
	return cs
}

// EVO-2139: testConnection must POST to the processor's test-connection
// endpoint (NOT its own /health), so real MCP handshakes actually run.
// This guards against a regression that re-adds the raw GET /health call.
func TestTestConnection_DelegatesToProcessor(t *testing.T) {
	cs := newCaptureTestServer(t, `{"success":true,"status_code":200,"response_time":0.42,"url_tested":"https://mcp.example/mcp","message":"Connection successful, discovered 3 tools","tools_count":3}`)
	svc := newServiceForTest(cs.URL)

	ctx := context.WithValue(context.Background(), "token", "tok-abc")

	result, err := svc.testConnection(ctx, "https://mcp.example/mcp", map[string]string{"Authorization": "Bearer sk-live"})
	if err != nil {
		t.Fatalf("testConnection: %v", err)
	}
	if want := "/api/v1/custom-mcp-servers/test-connection"; cs.gotPath != want {
		t.Fatalf("path: got %q want %q", cs.gotPath, want)
	}
	if !result.Success {
		t.Fatalf("want Success=true, got false (err=%q)", result.Error)
	}
	if result.URLTested != "https://mcp.example/mcp" {
		t.Fatalf("URLTested: got %q", result.URLTested)
	}
	if result.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode: got %d want 200", result.StatusCode)
	}
	if result.Message == "" {
		t.Fatalf("Message should be populated from processor response")
	}
	// Sanity: body must carry the MCP server URL + headers so the processor
	// can run the handshake against the real target, not against itself.
	body := string(cs.gotBody)
	if !strings.Contains(body, `"url":"https://mcp.example/mcp"`) {
		t.Fatalf("body missing url: %s", body)
	}
	if !strings.Contains(body, `"Authorization":"Bearer sk-live"`) {
		t.Fatalf("body missing MCP headers: %s", body)
	}
}

// EVO-2139: propagate X-Evo-Tenant-Id on the test call too, so the
// processor's runtime_context middleware (PY-1) can authorize it.
// Paridade com TestDiscoverTools_PropagatesTenantHeader_WhenBound.
// EVO-1739: the public TestConnection wrapper (test-before-save) must delegate to the
// same processor handshake and surface the TestResult unchanged.
func TestTestConnection_PublicWrapper_DelegatesAndReturnsResult(t *testing.T) {
	cs := newCaptureTestServer(t, `{"success":true,"status_code":200,"response_time":0.1,"url_tested":"https://mcp.example/mcp","message":"ok","tools_count":2}`)
	svc := newServiceForTest(cs.URL)
	ctx := context.WithValue(context.Background(), "token", "tok-abc")

	result, err := svc.TestConnection(ctx, "https://mcp.example/mcp", map[string]string{})
	if err != nil {
		t.Fatalf("TestConnection: %v", err)
	}
	if want := "/api/v1/custom-mcp-servers/test-connection"; cs.gotPath != want {
		t.Fatalf("path: got %q want %q", cs.gotPath, want)
	}
	if !result.Success || result.StatusCode != http.StatusOK {
		t.Fatalf("want success 200, got success=%v code=%d", result.Success, result.StatusCode)
	}
}

func TestTestConnection_PropagatesTenantHeader_WhenBound(t *testing.T) {
	cs := newCaptureTestServer(t, `{"success":true,"status_code":200,"tools_count":0}`)
	svc := newServiceForTest(cs.URL)

	tenantID := uuid.NewString()
	ctx := context.WithValue(context.Background(), "token", "tok-abc")
	ctx = runtimecontext.WithID(ctx, tenantID)

	if _, err := svc.testConnection(ctx, "https://mcp.example/mcp", map[string]string{}); err != nil {
		t.Fatalf("testConnection: %v", err)
	}
	if got := cs.gotHeaders.Get("X-Evo-Tenant-Id"); got != tenantID {
		t.Fatalf("X-Evo-Tenant-Id: got %q want %q", got, tenantID)
	}
}

// EVO-2139: without a bound tenant, the header MUST be omitted (same
// AC as discoverTools).
func TestTestConnection_OmitsTenantHeader_WhenUnbound(t *testing.T) {
	cs := newCaptureTestServer(t, `{"success":true,"status_code":200,"tools_count":0}`)
	svc := newServiceForTest(cs.URL)

	ctx := context.WithValue(context.Background(), "token", "tok-xyz")

	if _, err := svc.testConnection(ctx, "https://mcp.example/mcp", map[string]string{}); err != nil {
		t.Fatalf("testConnection: %v", err)
	}
	if _, present := cs.gotHeaders["X-Evo-Tenant-Id"]; present {
		t.Fatalf("X-Evo-Tenant-Id leaked when no tenant bound: %q", cs.gotHeaders.Get("X-Evo-Tenant-Id"))
	}
}

// EVO-2139: a failed handshake (processor returns success=false) must
// surface as TestResult.Success=false with the error propagated —
// NOT as a Go-level error that the handler turns into a 500.
func TestTestConnection_ReturnsFailureFromProcessor(t *testing.T) {
	cs := newCaptureTestServer(t, `{"success":false,"url_tested":"https://mcp.example/mcp","error":"connection refused","response_time":0.1}`)
	svc := newServiceForTest(cs.URL)

	ctx := context.WithValue(context.Background(), "token", "tok-abc")

	result, err := svc.testConnection(ctx, "https://mcp.example/mcp", map[string]string{})
	if err != nil {
		t.Fatalf("testConnection returned error, expected TestResult with Success=false: %v", err)
	}
	if result.Success {
		t.Fatalf("want Success=false, got true")
	}
	if result.Error != "connection refused" {
		t.Fatalf("Error: got %q want %q", result.Error, "connection refused")
	}
}
