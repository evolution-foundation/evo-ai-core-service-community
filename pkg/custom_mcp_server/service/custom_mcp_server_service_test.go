package service

import (
	"context"
	"net/http"
	"net/http/httptest"
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
