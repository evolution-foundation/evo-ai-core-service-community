package middleware

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"evo-ai-core-service/internal/services"

	"github.com/gin-gonic/gin"
)

func newAuthStub(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

func unreachableURL(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()
	return "http://" + addr
}

func withAuthContext(token, tokenType string) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := context.WithValue(c.Request.Context(), "token", token)
		ctx = context.WithValue(ctx, "token_type", tokenType)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

func hitProtectedRoute(t *testing.T, authURL string) (int, bool) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	var reached atomic.Bool
	router := gin.New()
	router.Use(withAuthContext("token", "bearer"))
	router.GET("/protected",
		NewPermissionMiddleware(authURL).RequirePermission("agents", "create"),
		func(c *gin.Context) {
			reached.Store(true)
			c.Status(http.StatusNoContent)
		},
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	router.ServeHTTP(rec, req)

	return rec.Code, reached.Load()
}

func TestRequirePermissionAllowsWhenGranted(t *testing.T) {
	stub := newAuthStub(t, http.StatusOK, `{"data":{"has_permission":true}}`)

	status, reached := hitProtectedRoute(t, stub.URL)
	if status != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", status)
	}
	if !reached {
		t.Fatal("expected handler to be reached")
	}
}

func TestRequirePermissionForbidsWhenDenied(t *testing.T) {
	stub := newAuthStub(t, http.StatusOK, `{"data":{"has_permission":false}}`)

	status, reached := hitProtectedRoute(t, stub.URL)
	if status != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", status)
	}
	if reached {
		t.Fatal("handler must not be reached when permission is denied")
	}
}

func TestRequirePermissionFailsClosedOnAuthFailures(t *testing.T) {
	t.Setenv(services.AllowMissingPermissionEndpointEnv, "")

	cases := []struct {
		name   string
		status int
		body   string
	}{
		{"500", http.StatusInternalServerError, `{"error":"boom"}`},
		{"502", http.StatusBadGateway, ``},
		{"503", http.StatusServiceUnavailable, ``},
		{"401", http.StatusUnauthorized, `{"error":"unauthorized"}`},
		{"403", http.StatusForbidden, `{"error":"forbidden"}`},
		{"404", http.StatusNotFound, `{"error":"not found"}`},
		{"200 data null", http.StatusOK, `{"data":null}`},
		{"200 invalid body", http.StatusOK, `not json`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := newAuthStub(t, tc.status, tc.body)

			status, reached := hitProtectedRoute(t, stub.URL)
			if status != http.StatusInternalServerError {
				t.Fatalf("expected 500, got %d", status)
			}
			if reached {
				t.Fatal("handler must not be reached when the permission check fails")
			}
		})
	}

	t.Run("unreachable auth service", func(t *testing.T) {
		status, reached := hitProtectedRoute(t, unreachableURL(t))
		if status != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d", status)
		}
		if reached {
			t.Fatal("handler must not be reached during an auth outage")
		}
	})
}

func TestRequirePermissionOptInOnlyCovers404(t *testing.T) {
	t.Setenv(services.AllowMissingPermissionEndpointEnv, "true")

	t.Run("404 allowed", func(t *testing.T) {
		stub := newAuthStub(t, http.StatusNotFound, ``)

		status, reached := hitProtectedRoute(t, stub.URL)
		if status != http.StatusNoContent || !reached {
			t.Fatalf("expected handler reached with 204, got %d reached=%v", status, reached)
		}
	})

	for _, code := range []int{http.StatusInternalServerError, http.StatusBadGateway, http.StatusUnauthorized} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			stub := newAuthStub(t, code, ``)

			status, reached := hitProtectedRoute(t, stub.URL)
			if status != http.StatusInternalServerError || reached {
				t.Fatalf("expected 500 and handler not reached, got %d reached=%v", status, reached)
			}
		})
	}
}

func TestRequirePermissionRejectsMissingToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var reached atomic.Bool
	router := gin.New()
	router.GET("/protected",
		NewPermissionMiddleware(unreachableURL(t)).RequirePermission("agents", "create"),
		func(c *gin.Context) { reached.Store(true) },
	)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/protected", nil))

	if rec.Code != http.StatusUnauthorized || reached.Load() {
		t.Fatalf("expected 401 and handler not reached, got %d reached=%v", rec.Code, reached.Load())
	}
}
