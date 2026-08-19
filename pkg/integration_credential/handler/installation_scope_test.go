package handler

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"evo-ai-core-service/internal/middleware"
	"evo-ai-core-service/pkg/integration_credential/model"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Same invariant as the AI credentials registry: `scope: "installation"` needs
// installation_configs.manage, checked on the server.
//// These drive the real handler with a stubbed permission middleware, so a gate
// that stops being called fails them.

type contextKey string

// scopeStubService lets each test decide what the stored credential looks like,
// which is what the "already installation-scoped" half of the gate reads.
type scopeStubService struct {
	stubService
	stored      *model.IntegrationCredential
	getErr      error
	createCalls int
	updateCalls int
	deleteCalls int
}

func (s *scopeStubService) Create(ctx context.Context, request model.IntegrationCredential) (*model.IntegrationCredential, error) {
	s.createCalls++
	return s.stubService.Create(ctx, request)
}

func (s *scopeStubService) GetByID(_ context.Context, id uuid.UUID) (*model.IntegrationCredential, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	if s.stored == nil {
		return &model.IntegrationCredential{Scope: model.ScopeAccount}, nil
	}
	stored := *s.stored
	stored.ID = id
	return &stored, nil
}

func (s *scopeStubService) Update(ctx context.Context, request *model.IntegrationCredential, isActive *bool, id uuid.UUID) (*model.IntegrationCredential, error) {
	s.updateCalls++
	return s.stubService.Update(ctx, request, isActive, id)
}

func (s *scopeStubService) Delete(_ context.Context, _ uuid.UUID) error {
	s.deleteCalls++
	return nil
}

type permissionStub struct {
	granted bool
	err     error
	asked   []string
}

func (p *permissionStub) RequirePermission(_, _ string) gin.HandlerFunc {
	return func(c *gin.Context) { c.Next() }
}

func (p *permissionStub) CheckPermission(_, _ string) (bool, error) { return p.granted, p.err }

func (p *permissionStub) CheckPermissionWithType(_, _, _ string) (bool, error) {
	return p.granted, p.err
}

func (p *permissionStub) HasPermission(_ *gin.Context, resource, action string) (bool, error) {
	p.asked = append(p.asked, resource+"."+action)
	return p.granted, p.err
}

func withPermission(t *testing.T, granted bool, err error) *permissionStub {
	t.Helper()
	stub := &permissionStub{granted: granted, err: err}
	t.Cleanup(middleware.SetGlobalPermissionMiddleware(stub))
	return stub
}

func scopeRequest(method, body string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(method, "/integration-credentials", bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")

	// Same keys EvoAuthMiddleware writes; contextutils reads them by name.
	ctx := context.WithValue(c.Request.Context(), contextKey("token"), "token-do-admin-de-conta")
	ctx = context.WithValue(ctx, contextKey("token_type"), "bearer")
	c.Request = c.Request.WithContext(ctx)

	return c, recorder
}

func newScopeHandler(stub *scopeStubService) IntegrationCredentialHandler {
	return NewIntegrationCredentialHandler(stub, fernetTestKey, nil, nil, nil)
}

func TestCreateInstallationScopeRefusedWithoutManage(t *testing.T) {
	permissions := withPermission(t, false, nil)
	stub := &scopeStubService{}
	handler := newScopeHandler(stub)

	c, recorder := scopeRequest(http.MethodPost,
		`{"name":"ElevenLabs da casa","provider":"elevenlabs","scope":"installation","value":"el-key-7a3c"}`)
	handler.Create(c)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", recorder.Code, recorder.Body.String())
	}
	if stub.createCalls != 0 {
		t.Error("the installation credential was created without installation_configs.manage")
	}
	if len(permissions.asked) == 0 || permissions.asked[0] != "installation_configs.manage" {
		t.Errorf("gate never consulted installation_configs.manage, asked=%v", permissions.asked)
	}
}

func TestCreateInstallationScopeAllowedWithManage(t *testing.T) {
	withPermission(t, true, nil)
	stub := &scopeStubService{}
	handler := newScopeHandler(stub)

	c, recorder := scopeRequest(http.MethodPost,
		`{"name":"ElevenLabs da casa","provider":"elevenlabs","scope":"installation","value":"el-key-7a3c"}`)
	handler.Create(c)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", recorder.Code, recorder.Body.String())
	}
	if stub.createCalls != 1 {
		t.Errorf("create calls = %d, want 1", stub.createCalls)
	}
}

func TestCreateAccountScopeNeedsNoInstallationPermission(t *testing.T) {
	permissions := withPermission(t, false, nil)
	stub := &scopeStubService{}
	handler := newScopeHandler(stub)

	c, recorder := scopeRequest(http.MethodPost,
		`{"name":"Dify da conta","provider":"dify","value":"app-9c1d"}`)
	handler.Create(c)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", recorder.Code, recorder.Body.String())
	}
	if len(permissions.asked) != 0 {
		t.Errorf("account write consulted the installation gate: %v", permissions.asked)
	}
}

func TestUpdatePromotingToInstallationRefusedWithoutManage(t *testing.T) {
	withPermission(t, false, nil)
	stub := &scopeStubService{stored: &model.IntegrationCredential{Scope: model.ScopeAccount}}
	handler := newScopeHandler(stub)

	c, recorder := scopeRequest(http.MethodPut,
		`{"name":"Promovida","provider":"dify","scope":"installation"}`)
	c.Params = gin.Params{{Key: "id", Value: uuid.New().String()}}
	handler.Update(c)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", recorder.Code, recorder.Body.String())
	}
	if stub.updateCalls != 0 {
		t.Error("an account credential was promoted to installation without the privilege")
	}
}

// An omitted scope keeps the stored one, so editing a credential that ALREADY
// is the installation default must be gated by the stored value.
func TestUpdateOfStoredInstallationCredentialRefusedWithoutManage(t *testing.T) {
	withPermission(t, false, nil)
	stub := &scopeStubService{stored: &model.IntegrationCredential{Scope: model.ScopeInstallation}}
	handler := newScopeHandler(stub)

	c, recorder := scopeRequest(http.MethodPut,
		`{"name":"Renomeada por quem nao pode","provider":"dify"}`)
	c.Params = gin.Params{{Key: "id", Value: uuid.New().String()}}
	handler.Update(c)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", recorder.Code, recorder.Body.String())
	}
	if stub.updateCalls != 0 {
		t.Error("the stored installation credential was edited without the privilege")
	}
}

func TestDeleteOfInstallationCredentialRefusedWithoutManage(t *testing.T) {
	withPermission(t, false, nil)
	stub := &scopeStubService{stored: &model.IntegrationCredential{Scope: model.ScopeInstallation}}
	handler := newScopeHandler(stub)

	c, recorder := scopeRequest(http.MethodDelete, "")
	c.Params = gin.Params{{Key: "id", Value: uuid.New().String()}}
	handler.Delete(c)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", recorder.Code, recorder.Body.String())
	}
	if stub.deleteCalls != 0 {
		t.Error("the installation credential was deleted without the privilege")
	}
}

func TestInstallationScopeDeniedWhenPermissionCheckFails(t *testing.T) {
	withPermission(t, true, context.DeadlineExceeded)
	stub := &scopeStubService{}
	handler := newScopeHandler(stub)

	c, recorder := scopeRequest(http.MethodPost,
		`{"name":"ElevenLabs da casa","provider":"elevenlabs","scope":"installation","value":"el-key-7a3c"}`)
	handler.Create(c)

	if recorder.Code == http.StatusCreated {
		t.Fatal("a failing permission check let the installation write through")
	}
	if stub.createCalls != 0 {
		t.Error("service was reached despite the failed permission check")
	}
}

// A target the gate cannot read demands the privilege, as in the api_key handler.
func TestDeleteDemandsTheGateWhenTheTargetCannotBeRead(t *testing.T) {
	withPermission(t, false, nil)
	stub := &scopeStubService{getErr: errors.New("connection reset by peer")}
	handler := newScopeHandler(stub)

	c, recorder := scopeRequest(http.MethodDelete, "")
	c.Params = gin.Params{{Key: "id", Value: uuid.New().String()}}
	handler.Delete(c)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", recorder.Code, recorder.Body.String())
	}
	if stub.deleteCalls != 0 {
		t.Error("the credential was deleted while the gate could not read it")
	}
}
