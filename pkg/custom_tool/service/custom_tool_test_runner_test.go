package service

import (
	"context"
	"encoding/json"
	"errors"
	apiErrors "evo-ai-core-service/internal/httpclient/errors"
	model "evo-ai-core-service/pkg/custom_tool/model"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// TestMain enables loopback so httptest.NewServer-backed tests can run.
// SSRF rejection tests explicitly flip this off (see withSSRFGuard).
func TestMain(m *testing.M) {
	allowLoopbackForTests = true
	code := m.Run()
	allowLoopbackForTests = false
	os.Exit(code)
}

// withSSRFGuard runs fn with loopback rejected, mirroring production.
func withSSRFGuard(t *testing.T, fn func()) {
	t.Helper()
	prev := allowLoopbackForTests
	allowLoopbackForTests = false
	defer func() { allowLoopbackForTests = prev }()
	fn()
}

func TestRunToolTest_SuccessJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	res := runToolTest(context.Background(), "GET", srv.URL, nil, nil)
	if !res.Success {
		t.Fatalf("expected success, got error: %s", res.Error)
	}
	if res.StatusCode != 200 {
		t.Fatalf("expected status 200, got %d", res.StatusCode)
	}
	if res.Body != `{"ok":true}` {
		t.Fatalf("body mismatch: %q", res.Body)
	}
	if res.Headers["Content-Type"] != "application/json" {
		t.Fatalf("missing/wrong content-type header: %v", res.Headers)
	}
	if res.ResponseTime < 0 {
		t.Fatalf("response time must be non-negative")
	}
}

// THE bug case: webhook.site responds HTML by default. Old code
// blindly json.Unmarshal'd the body and reported success=false with
// "invalid character '<'". New code must return success=true with the
// raw body, since the HTTP request itself was 200 OK.
func TestRunToolTest_SuccessHTML_NoUnmarshalAttempt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html><body>hi</body></html>"))
	}))
	defer srv.Close()

	res := runToolTest(context.Background(), "GET", srv.URL, nil, nil)
	if !res.Success {
		t.Fatalf("HTML 200 must be success, got error: %s", res.Error)
	}
	if res.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
	if !strings.Contains(res.Body, "<html>") {
		t.Fatalf("body must contain raw HTML, got %q", res.Body)
	}
	if strings.Contains(res.Error, "invalid character") {
		t.Fatalf("must not bubble json parse errors anymore; got: %s", res.Error)
	}
}

func TestRunToolTest_SuccessPlainText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("hello world"))
	}))
	defer srv.Close()

	res := runToolTest(context.Background(), "GET", srv.URL, nil, nil)
	if !res.Success {
		t.Fatalf("plain text 200 must be success, got error: %s", res.Error)
	}
	if res.Body != "hello world" {
		t.Fatalf("body mismatch: %q", res.Body)
	}
}

func TestRunToolTest_SuccessNon200_2xx(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   []byte
	}{
		{"201 created with body", 201, []byte(`{"id":1}`)},
		{"204 no content empty body", 204, nil},
		{"202 accepted", 202, []byte("queued")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				if tc.body != nil {
					_, _ = w.Write(tc.body)
				}
			}))
			defer srv.Close()

			res := runToolTest(context.Background(), "POST", srv.URL, nil, nil)
			if !res.Success {
				t.Fatalf("status %d must be success (2xx); error=%s", tc.status, res.Error)
			}
			if res.StatusCode != tc.status {
				t.Fatalf("expected %d, got %d", tc.status, res.StatusCode)
			}
		})
	}
}

func TestRunToolTest_Failure4xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad token"}`))
	}))
	defer srv.Close()

	res := runToolTest(context.Background(), "GET", srv.URL, nil, nil)
	if res.Success {
		t.Fatalf("401 must NOT be success")
	}
	if res.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", res.StatusCode)
	}
	if res.Body != `{"error":"bad token"}` {
		t.Fatalf("body should be preserved on failure too, got %q", res.Body)
	}
	if !strings.Contains(res.Error, "401") {
		t.Fatalf("expected error to mention HTTP code, got %q", res.Error)
	}
}

func TestRunToolTest_Failure5xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	res := runToolTest(context.Background(), "GET", srv.URL, nil, nil)
	if res.Success {
		t.Fatalf("500 must NOT be success")
	}
	if res.StatusCode != 500 {
		t.Fatalf("expected 500, got %d", res.StatusCode)
	}
}

func TestRunToolTest_NetworkError_DNS(t *testing.T) {
	res := runToolTest(context.Background(), "GET", "http://nonexistent.invalid.test/", nil, nil)
	if res.Success {
		t.Fatalf("DNS failure must NOT be success")
	}
	if res.StatusCode != 0 {
		t.Fatalf("expected 0 status code when no response, got %d", res.StatusCode)
	}
	if res.Error == "" {
		t.Fatalf("must surface an error message")
	}
}

func TestRunToolTest_HeadersSent(t *testing.T) {
	var gotAuth, gotCustom string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotCustom = r.Header.Get("X-Evo-Teste")
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	headers := map[string]string{
		"Authorization": "Bearer abc",
		"X-Evo-Teste":   "valor-1",
	}
	res := runToolTest(context.Background(), "GET", srv.URL, headers, nil)
	if !res.Success {
		t.Fatalf("expected success, got error: %s", res.Error)
	}
	if gotAuth != "Bearer abc" {
		t.Fatalf("auth header not sent; got %q", gotAuth)
	}
	if gotCustom != "valor-1" {
		t.Fatalf("custom header not sent; got %q", gotCustom)
	}
}

func TestRunToolTest_MethodPropagation(t *testing.T) {
	for _, method := range []string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"} {
		t.Run(method, func(t *testing.T) {
			var gotMethod string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod = r.Method
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()

			res := runToolTest(context.Background(), method, srv.URL, nil, nil)
			if !res.Success {
				t.Fatalf("%s expected success, got %s", method, res.Error)
			}
			if gotMethod != method {
				t.Fatalf("server saw method %q, expected %q", gotMethod, method)
			}
		})
	}
}

func TestRunToolTest_ResponseTimeIsPositiveOnSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	res := runToolTest(context.Background(), "GET", srv.URL, nil, nil)
	// Windows timer resolution can round sub-microsecond local loopback to 0;
	// the contract we care about is "measured and non-negative".
	if res.ResponseTime < 0 {
		t.Fatalf("response time must be non-negative, got %f", res.ResponseTime)
	}
}

// ---- NEW: body propagation for POST/PUT/PATCH ----

func TestRunToolTest_BodySentOnPOST(t *testing.T) {
	var gotBody []byte
	var gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		gotContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	body := map[string]interface{}{"nome": "marcelo", "pedido": 123}
	res := runToolTest(context.Background(), "POST", srv.URL, nil, body)
	if !res.Success {
		t.Fatalf("expected success, got %s", res.Error)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(gotBody, &decoded); err != nil {
		t.Fatalf("server body not valid JSON: %v / raw=%q", err, gotBody)
	}
	if decoded["nome"] != "marcelo" {
		t.Fatalf("body field 'nome' missing/wrong: %v", decoded)
	}
	if gotContentType != "application/json" {
		t.Fatalf("expected default Content-Type application/json, got %q", gotContentType)
	}
}

func TestRunToolTest_BodySentOnPUT_AndPATCH(t *testing.T) {
	for _, method := range []string{"PUT", "PATCH"} {
		t.Run(method, func(t *testing.T) {
			var gotBody []byte
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotBody, _ = io.ReadAll(r.Body)
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()
			body := map[string]interface{}{"k": "v"}
			res := runToolTest(context.Background(), method, srv.URL, nil, body)
			if !res.Success {
				t.Fatalf("%s expected success, got %s", method, res.Error)
			}
			if !strings.Contains(string(gotBody), `"k":"v"`) {
				t.Fatalf("%s body did not arrive at server: %q", method, gotBody)
			}
		})
	}
}

func TestRunToolTest_BodyNotSentOnGET(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	res := runToolTest(context.Background(), "GET", srv.URL, nil, map[string]interface{}{"would": "be ignored"})
	if !res.Success {
		t.Fatalf("expected success, got %s", res.Error)
	}
	if len(gotBody) != 0 {
		t.Fatalf("GET must not carry body; got %q", gotBody)
	}
}

func TestRunToolTest_CustomContentTypeRespected(t *testing.T) {
	var gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	headers := map[string]string{"Content-Type": "application/vnd.custom+json"}
	res := runToolTest(context.Background(), "POST", srv.URL, headers, map[string]interface{}{"x": 1})
	if !res.Success {
		t.Fatalf("expected success, got %s", res.Error)
	}
	if gotContentType != "application/vnd.custom+json" {
		t.Fatalf("user Content-Type must win; got %q", gotContentType)
	}
}

// ---- NEW: SSRF defenses ----

func TestValidateEndpoint_RejectsPrivateHosts(t *testing.T) {
	cases := []string{
		"http://127.0.0.1/",
		"http://localhost/",
		"http://169.254.169.254/latest/meta-data/",
		"http://10.0.0.1/",
		"http://172.16.0.1/",
		"http://192.168.1.1/",
		"http://0.0.0.0/",
		"http://[::1]/",
		"http://[fe80::1]/",
	}
	withSSRFGuard(t, func() {
		for _, raw := range cases {
			t.Run(raw, func(t *testing.T) {
				res := runToolTest(context.Background(), "GET", raw, nil, nil)
				if res.Success {
					t.Fatalf("must reject private host %s", raw)
				}
				if res.StatusCode != 0 {
					t.Fatalf("must not perform the request (status should be 0); got %d", res.StatusCode)
				}
				if !strings.Contains(strings.ToLower(res.Error), "interno") &&
					!strings.Contains(strings.ToLower(res.Error), "privado") {
					t.Fatalf("error should mention internal/private; got %q", res.Error)
				}
			})
		}
	})
}

func TestValidateEndpoint_RejectsBadSchemes(t *testing.T) {
	for _, raw := range []string{"file:///etc/passwd", "gopher://x/", "javascript:alert(1)"} {
		res := runToolTest(context.Background(), "GET", raw, nil, nil)
		if res.Success {
			t.Fatalf("must reject scheme: %s", raw)
		}
		if !strings.Contains(res.Error, "scheme") && !strings.Contains(res.Error, "URL") {
			t.Fatalf("error should mention scheme/URL; got %q", res.Error)
		}
	}
}

func TestRunToolTest_RedirectsAreNotFollowed(t *testing.T) {
	// Server replies 302 with a Location header. We must surface the 302
	// as-is (not follow it). httptest binds to 127.0.0.1 — loopback is
	// allowed in tests via TestMain.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "http://example.org/redirected")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()

	res := runToolTest(context.Background(), "GET", srv.URL, nil, nil)
	// 302 is not 2xx => Success=false; this proves we did NOT follow.
	if res.Success {
		t.Fatalf("redirect must be surfaced as-is (302), not followed")
	}
	if res.StatusCode != http.StatusFound {
		t.Fatalf("expected 302, got %d", res.StatusCode)
	}
	if res.Headers["Location"] != "http://example.org/redirected" {
		t.Fatalf("Location header must be surfaced to the user; got headers=%v", res.Headers)
	}
}

func TestRunToolTest_BodyTruncatedAt1MiB(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		big := strings.Repeat("a", (1<<20)+100) // 1 MiB + 100 bytes
		_, _ = w.Write([]byte(big))
	}))
	defer srv.Close()

	res := runToolTest(context.Background(), "GET", srv.URL, nil, nil)
	if !res.Success {
		t.Fatalf("expected success, got %s", res.Error)
	}
	if int64(len(res.Body)) != 1<<20 {
		t.Fatalf("body should be capped at 1 MiB, got %d bytes", len(res.Body))
	}
	if res.Headers["X-Evo-Body-Truncated"] == "" {
		t.Fatalf("truncation should be signaled via X-Evo-Body-Truncated header")
	}
}

func TestRunToolTest_DropsSensitiveResponseHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Set-Cookie", "session=secret")
		w.Header().Set("Authorization", "Bearer leaked")
		w.Header().Set("WWW-Authenticate", "Basic realm=x")
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	res := runToolTest(context.Background(), "GET", srv.URL, nil, nil)
	if !res.Success {
		t.Fatalf("expected success, got %s", res.Error)
	}
	if _, ok := res.Headers["Set-Cookie"]; ok {
		t.Fatalf("Set-Cookie must be dropped, got %v", res.Headers)
	}
	if _, ok := res.Headers["Authorization"]; ok {
		t.Fatalf("Authorization must be dropped")
	}
	if _, ok := res.Headers["Www-Authenticate"]; ok {
		t.Fatalf("WWW-Authenticate must be dropped")
	}
	if res.Headers["Content-Type"] != "text/plain" {
		t.Fatalf("Content-Type must remain; headers=%v", res.Headers)
	}
}

func TestIsPublicIP(t *testing.T) {
	withSSRFGuard(t, func() {
		testIsPublicIPBody(t)
	})
}

func testIsPublicIPBody(t *testing.T) {
	pub := []string{"8.8.8.8", "1.1.1.1", "2001:4860:4860::8888"}
	priv := []string{"127.0.0.1", "::1", "10.1.2.3", "172.20.0.1", "192.168.0.1", "169.254.169.254", "0.0.0.0", "fc00::1", "fe80::1"}
	for _, s := range pub {
		ip := net.ParseIP(s)
		if ip == nil {
			t.Fatalf("invalid test ip %q", s)
		}
		if !isPublicIP(ip) {
			t.Errorf("%s should be public", s)
		}
	}
	for _, s := range priv {
		ip := net.ParseIP(s)
		if ip == nil {
			t.Fatalf("invalid test ip %q", s)
		}
		if isPublicIP(ip) {
			t.Errorf("%s should be NON-public", s)
		}
	}
}

// EVO-1738: the public TestPayload wrapper (test-before-save) runs the same request
// as Test against an UNSAVED payload, and fails fast on an unsupported method.
func TestTestPayload_UnsavedTool_RunsAndValidatesMethod(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	svc := &customToolService{}
	res, err := svc.TestPayload(context.Background(), model.CustomToolTestPayloadRequest{
		Method:   "get",
		Endpoint: srv.URL,
	})
	if err != nil {
		t.Fatalf("TestPayload: %v", err)
	}
	if !res.Success || res.StatusCode != http.StatusOK {
		t.Fatalf("want success 200, got success=%v code=%d err=%q", res.Success, res.StatusCode, res.Error)
	}

	_, err = svc.TestPayload(context.Background(), model.CustomToolTestPayloadRequest{
		Method:   "TRACE",
		Endpoint: srv.URL,
	})
	if err == nil {
		t.Fatal("expected error for unsupported method")
	}
	// R1 review: an unsupported method is caller input — it must map to 400, not 500.
	var apiErr *apiErrors.ApiError
	if !errors.As(err, &apiErr) {
		t.Fatalf("want *ApiError, got %T", err)
	}
	if apiErr.HTTPCode != http.StatusBadRequest {
		t.Fatalf("want HTTP 400 for unsupported method, got %d", apiErr.HTTPCode)
	}
}

// R1 review (EVO-1738): the tool's path placeholders and query params must reach the
// wire. Before resolveToolURL the test hit `/users/{user_id}` literally and dropped
// query params entirely, so "Request OK — HTTP 200" could describe a request that
// carried none of the user's configuration.
func TestTestPayload_AppliesPathAndQueryParams(t *testing.T) {
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.Query().Encode()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	svc := &customToolService{}
	res, err := svc.TestPayload(context.Background(), model.CustomToolTestPayloadRequest{
		Method:      "GET",
		Endpoint:    srv.URL + "/users/{user_id}/posts",
		PathParams:  map[string]string{"user_id": "42"},
		QueryParams: map[string]interface{}{"limit": 10, "q": "hello world"},
	})
	if err != nil {
		t.Fatalf("TestPayload: %v", err)
	}
	if !res.Success {
		t.Fatalf("want success, got err=%q", res.Error)
	}
	if gotPath != "/users/42/posts" {
		t.Fatalf("path placeholder not substituted: got %q", gotPath)
	}
	if gotQuery != "limit=10&q=hello+world" {
		t.Fatalf("query params not applied: got %q", gotQuery)
	}
}

// R1 review: a path placeholder must not become an SSRF bypass. Two layers hold:
// the value is percent-escaped, so it cannot introduce a scheme/authority; and
// validateEndpoint runs on the RESOLVED url, so anything that did slip through
// would still face the public-IP gate.
func TestResolveToolURL_PlaceholderCannotInjectAHost(t *testing.T) {
	if got := resolveToolURL("https://x.test/{p}", map[string]string{"p": "a b"}, nil); got != "https://x.test/a%20b" {
		t.Fatalf("path value not escaped: got %q", got)
	}

	// The classic metadata-endpoint attempt: escaping neuters "://" so no authority
	// is ever produced — the value stays an opaque path segment.
	resolved := resolveToolURL("{host}/latest/meta-data/", map[string]string{"host": "http://169.254.169.254"}, nil)
	if strings.Contains(resolved, "//169.254.169.254") {
		t.Fatalf("placeholder injected an authority: %q", resolved)
	}

	svc := &customToolService{}
	res, err := svc.TestPayload(context.Background(), model.CustomToolTestPayloadRequest{
		Method:     "GET",
		Endpoint:   "{host}/latest/meta-data/",
		PathParams: map[string]string{"host": "http://169.254.169.254"},
	})
	if err != nil {
		t.Fatalf("TestPayload: %v", err)
	}
	if res.Success {
		t.Fatalf("placeholder-built internal URL was not blocked: err=%q", res.Error)
	}

	// And a literal internal endpoint still trips the public-IP gate itself.
	withSSRFGuard(t, func() {
		blocked := runToolTest(context.Background(), "GET", "http://169.254.169.254/latest/meta-data/", nil, nil)
		if blocked.Success || blocked.Error != errBlockedHost.Error() {
			t.Fatalf("want blocked-host error, got success=%v err=%q", blocked.Success, blocked.Error)
		}
	})
}

// CRM-527: body params are stored as {type, required, description} — the
// declaration of what the AI will send. Posting that declaration made the Test
// button report on a request no agent ever makes.
func TestRunToolTest_SchemaBodyParamsAreSentAsSampleValues(t *testing.T) {
	var received map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &received)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	body := map[string]interface{}{
		"queryText": map[string]interface{}{"type": "string", "required": true, "description": "q"},
		"topK":      map[string]interface{}{"type": "number", "required": false, "description": "n"},
		"deep":      map[string]interface{}{"type": "boolean", "required": true, "description": ""},
	}

	if res := runToolTest(context.Background(), http.MethodPost, srv.URL, nil, body); !res.Success {
		t.Fatalf("expected success, got %+v", res)
	}

	if got, want := received["queryText"], "string"; got != want {
		t.Errorf("queryText = %#v, want %#v", got, want)
	}
	if got, want := received["topK"], float64(0); got != want {
		t.Errorf("topK = %#v, want %#v", got, want)
	}
	if got, want := received["deep"], false; got != want {
		t.Errorf("deep = %#v, want %#v", got, want)
	}
}

// A literal body the user typed is not a schema and must reach the endpoint
// untouched — including an object that happens to carry a `type` key.
func TestRunToolTest_LiteralBodyParamsAreSentVerbatim(t *testing.T) {
	var received map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &received)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	body := map[string]interface{}{
		"legacy": "{query}",
		"filter": map[string]interface{}{"type": "premium", "since": "2026-01-01"},
		"limit":  float64(10),
	}

	if res := runToolTest(context.Background(), http.MethodPost, srv.URL, nil, body); !res.Success {
		t.Fatalf("expected success, got %+v", res)
	}

	if got, want := received["legacy"], "{query}"; got != want {
		t.Errorf("legacy = %#v, want %#v", got, want)
	}
	filter, ok := received["filter"].(map[string]interface{})
	if !ok || filter["type"] != "premium" || filter["since"] != "2026-01-01" {
		t.Errorf("filter = %#v, want the literal object", received["filter"])
	}
	if got, want := received["limit"], float64(10); got != want {
		t.Errorf("limit = %#v, want %#v", got, want)
	}
}
