package services

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newPermissionServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/permissions/check" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

func checkAgainst(t *testing.T, server *httptest.Server) (bool, error) {
	t.Helper()
	svc := NewEvoAuthService(server.URL)
	return svc.CheckPermission(context.Background(), "token", "agents.create", "bearer")
}

func TestCheckPermissionGrantedOn200(t *testing.T) {
	server := newPermissionServer(t, http.StatusOK, `{"data":{"has_permission":true}}`)

	allowed, err := checkAgainst(t, server)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Fatal("expected permission to be granted")
	}
}

func TestCheckPermissionDeniedOn200(t *testing.T) {
	server := newPermissionServer(t, http.StatusOK, `{"data":{"has_permission":false}}`)

	allowed, err := checkAgainst(t, server)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allowed {
		t.Fatal("expected permission to be denied")
	}
}

func TestCheckPermissionFailsClosed(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{"500", http.StatusInternalServerError, `{"error":"boom"}`},
		{"502", http.StatusBadGateway, `<html>bad gateway</html>`},
		{"503", http.StatusServiceUnavailable, ``},
		{"401", http.StatusUnauthorized, `{"error":"unauthorized"}`},
		{"403", http.StatusForbidden, `{"error":"forbidden"}`},
		{"400", http.StatusBadRequest, `{"error":"bad request"}`},
		{"invalid 200 body", http.StatusOK, `not json`},
		{"200 with data null", http.StatusOK, `{"data":null}`},
		{"200 with null body", http.StatusOK, `null`},
		{"200 with data not an object", http.StatusOK, `{"data":"yes"}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := newPermissionServer(t, tc.status, tc.body)

			allowed, err := checkAgainst(t, server)
			if err == nil {
				t.Fatal("expected an error")
			}
			if allowed {
				t.Fatal("expected permission to be denied")
			}
		})
	}
}

func TestCheckPermissionDeniesWhenUnreachable(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()

	svc := NewEvoAuthService("http://" + addr)
	allowed, err := svc.CheckPermission(context.Background(), "token", "agents.create", "bearer")
	if err == nil {
		t.Fatal("expected an error")
	}
	var networkErr *NetworkError
	if !errors.As(err, &networkErr) {
		t.Fatalf("expected NetworkError, got %T: %v", err, err)
	}
	if allowed {
		t.Fatal("expected permission to be denied")
	}
}

func TestCheckPermissionDeniesOn404WithoutOptIn(t *testing.T) {
	t.Setenv(AllowMissingPermissionEndpointEnv, "")
	server := newPermissionServer(t, http.StatusNotFound, `{"error":"not found"}`)

	allowed, err := checkAgainst(t, server)
	if err == nil {
		t.Fatal("expected an error")
	}
	var notImplemented *NotImplementedError
	if !errors.As(err, &notImplemented) {
		t.Fatalf("expected NotImplementedError, got %T: %v", err, err)
	}
	if allowed {
		t.Fatal("expected permission to be denied")
	}
}

func TestCheckPermissionDeniesOn404WithNonTrueOptIn(t *testing.T) {
	for _, value := range []string{"1", "TRUE", "yes", " true"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv(AllowMissingPermissionEndpointEnv, value)
			server := newPermissionServer(t, http.StatusNotFound, ``)

			allowed, err := checkAgainst(t, server)
			if err == nil || allowed {
				t.Fatalf("expected denial, got allowed=%v err=%v", allowed, err)
			}
		})
	}
}

func TestCheckPermissionAllowsOn404WithOptIn(t *testing.T) {
	t.Setenv(AllowMissingPermissionEndpointEnv, "true")
	server := newPermissionServer(t, http.StatusNotFound, `{"error":"not found"}`)

	allowed, err := checkAgainst(t, server)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Fatal("expected permission to be granted under opt-in")
	}
}

func TestCheckPermissionOptInDoesNotCoverOtherFailures(t *testing.T) {
	t.Setenv(AllowMissingPermissionEndpointEnv, "true")

	for _, status := range []int{http.StatusInternalServerError, http.StatusBadGateway, http.StatusUnauthorized} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := newPermissionServer(t, status, `{}`)

			allowed, err := checkAgainst(t, server)
			if err == nil {
				t.Fatal("expected an error")
			}
			if allowed {
				t.Fatal("expected permission to be denied")
			}
		})
	}

	t.Run("unreachable", func(t *testing.T) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		addr := listener.Addr().String()
		_ = listener.Close()

		svc := NewEvoAuthService("http://" + addr)
		allowed, err := svc.CheckPermission(context.Background(), "token", "agents.create", "bearer")
		if err == nil || allowed {
			t.Fatalf("expected denial, got allowed=%v err=%v", allowed, err)
		}
	})
}

func TestSiblingPermissionChecksFailClosed(t *testing.T) {
	t.Setenv(AllowMissingPermissionEndpointEnv, "")

	for _, status := range []int{http.StatusOK, http.StatusNotFound, http.StatusInternalServerError, http.StatusUnauthorized} {
		body := `{"data":null}`
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}))
		svc := NewEvoAuthService(server.URL)

		allowed, err := svc.CheckAccountPermission(context.Background(), "user", "account", "agents.create", "token", "bearer")
		if err == nil || allowed {
			t.Fatalf("CheckAccountPermission status %d: expected denial, got allowed=%v err=%v", status, allowed, err)
		}

		allowed, err = svc.CheckUserPermission(context.Background(), "user", "agents.create", "token", "bearer")
		if err == nil || allowed {
			t.Fatalf("CheckUserPermission status %d: expected denial, got allowed=%v err=%v", status, allowed, err)
		}

		server.Close()
	}
}

func TestValidateTokenMapsOutageAndMissingEndpointToServiceUnavailable(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusServiceUnavailable} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
		}))
		svc := NewEvoAuthService(server.URL)

		data, err := svc.ValidateToken("token", "bearer")
		if data != nil {
			t.Fatalf("status %d: expected nil token data", status)
		}
		var unavailable *ServiceUnavailableError
		if !errors.As(err, &unavailable) {
			t.Fatalf("status %d: expected ServiceUnavailableError, got %T: %v", status, err, err)
		}

		server.Close()
	}
}

func TestValidateTokenKeepsAuthenticationErrorOn401(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	svc := NewEvoAuthService(server.URL)

	_, err := svc.ValidateToken("token", "bearer")
	var authErr *AuthenticationError
	if !errors.As(err, &authErr) {
		t.Fatalf("expected AuthenticationError, got %T: %v", err, err)
	}
}
