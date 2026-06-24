package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunToolTest_SuccessJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	res := runToolTest(context.Background(), "GET", srv.URL, nil)
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

	res := runToolTest(context.Background(), "GET", srv.URL, nil)
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

	res := runToolTest(context.Background(), "GET", srv.URL, nil)
	if !res.Success {
		t.Fatalf("plain text 200 must be success, got error: %s", res.Error)
	}
	if res.Body != "hello world" {
		t.Fatalf("body mismatch: %q", res.Body)
	}
}

func TestRunToolTest_SuccessNon200_2xx(t *testing.T) {
	// 201 Created and 204 No Content both count as success.
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

			res := runToolTest(context.Background(), "POST", srv.URL, nil)
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

	res := runToolTest(context.Background(), "GET", srv.URL, nil)
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

	res := runToolTest(context.Background(), "GET", srv.URL, nil)
	if res.Success {
		t.Fatalf("500 must NOT be success")
	}
	if res.StatusCode != 500 {
		t.Fatalf("expected 500, got %d", res.StatusCode)
	}
}

func TestRunToolTest_NetworkError_DNS(t *testing.T) {
	// Use a non-routable .invalid TLD to force a DNS failure deterministically.
	res := runToolTest(context.Background(), "GET", "http://nonexistent.invalid.test/", nil)
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
	res := runToolTest(context.Background(), "GET", srv.URL, headers)
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

			res := runToolTest(context.Background(), method, srv.URL, nil)
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

	res := runToolTest(context.Background(), "GET", srv.URL, nil)
	if res.ResponseTime <= 0 {
		t.Fatalf("response time must be > 0 on a real call, got %f", res.ResponseTime)
	}
}
