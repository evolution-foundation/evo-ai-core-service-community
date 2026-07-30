package handler

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"evo-ai-core-service/internal/middleware"
	"evo-ai-core-service/pkg/api_key/model"
	"evo-ai-core-service/pkg/api_key/service"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Writing at the installation scope requires installation_configs.manage, and
// the browser check is not a gate.
//// These drive the real handler with a stubbed permission middleware, so a gate
// that stops being called fails them.

// contextKey mirrors how EvoAuthMiddleware stores the token: plain string keys
// read back by contextutils.
type contextKey string

type scopeStubService struct {
	service.ApiKeyService
	stored      *model.ApiKey
	getErr      error
	createCalls int
	updateCalls int
	deleteCalls int
}

func (s *scopeStubService) Create(_ context.Context, request model.ApiKey) (*model.ApiKey, error) {
	s.createCalls++
	request.ID = uuid.New()
	return &request, nil
}

func (s *scopeStubService) GetByID(_ context.Context, id uuid.UUID) (*model.ApiKey, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	if s.stored == nil {
		return nil, nil
	}
	stored := *s.stored
	stored.ID = id
	return &stored, nil
}

func (s *scopeStubService) Update(_ context.Context, request *model.ApiKey, _ *bool, id uuid.UUID) (*model.ApiKey, error) {
	s.updateCalls++
	stored := *request
	stored.ID = id
	return &stored, nil
}

func (s *scopeStubService) Delete(_ context.Context, _ uuid.UUID) (bool, error) {
	s.deleteCalls++
	return true, nil
}

// permissionStub answers a fixed verdict and records what was asked.
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
	restore := middleware.SetGlobalPermissionMiddleware(stub)
	t.Cleanup(restore)
	return stub
}

// requestWithToken builds a context carrying a bearer, which is what the real
// gate reads before asking the auth service.
func requestWithToken(method, body string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(method, "/agents/apikeys", bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")

	// Same keys EvoAuthMiddleware writes; contextutils reads them by name.
	ctx := context.WithValue(c.Request.Context(), contextKey("token"), "token-do-admin-de-conta")
	ctx = context.WithValue(ctx, contextKey("token_type"), "bearer")
	c.Request = c.Request.WithContext(ctx)

	return c, recorder
}

func newScopeHandler(stub *scopeStubService) ApiKeyHandler {
	return NewApiKeyHandler(stub, "cw_0x689RpI-jtRR7oE8h_eQsKImvJapLeSbXpwF4e4=")
}

func TestCreateInstallationScopeRefusedWithoutManage(t *testing.T) {
	permissions := withPermission(t, false, nil)
	stub := &scopeStubService{}
	handler := newScopeHandler(stub)

	c, recorder := requestWithToken(http.MethodPost,
		`{"name":"Chave da casa","provider":"openai","key_value":"sk-abcdef","scope":"installation"}`)
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

	c, recorder := requestWithToken(http.MethodPost,
		`{"name":"Chave da casa","provider":"openai","key_value":"sk-abcdef","scope":"installation"}`)
	handler.Create(c)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", recorder.Code, recorder.Body.String())
	}
	if stub.createCalls != 1 {
		t.Errorf("create calls = %d, want 1", stub.createCalls)
	}
}

// The account scope is the common case and must stay untouched by the gate.
func TestCreateAccountScopeNeedsNoInstallationPermission(t *testing.T) {
	permissions := withPermission(t, false, nil)
	stub := &scopeStubService{}
	handler := newScopeHandler(stub)

	c, recorder := requestWithToken(http.MethodPost,
		`{"name":"Da conta","provider":"openai","key_value":"sk-abcdef"}`)
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
	stub := &scopeStubService{stored: &model.ApiKey{Scope: model.ScopeAccount}}
	handler := newScopeHandler(stub)

	c, recorder := requestWithToken(http.MethodPut,
		`{"name":"Promovida","provider":"openai","scope":"installation"}`)
	c.Params = gin.Params{{Key: "id", Value: uuid.New().String()}}
	handler.Update(c)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", recorder.Code, recorder.Body.String())
	}
	if stub.updateCalls != 0 {
		t.Error("an account credential was promoted to installation without the privilege")
	}
}

// The nastier half: an omitted scope keeps the stored one, so editing a
// credential that ALREADY is the installation default must be gated too.
func TestUpdateOfStoredInstallationCredentialRefusedWithoutManage(t *testing.T) {
	withPermission(t, false, nil)
	stub := &scopeStubService{stored: &model.ApiKey{Scope: model.ScopeInstallation}}
	handler := newScopeHandler(stub)

	c, recorder := requestWithToken(http.MethodPut,
		`{"name":"Renomeada por quem nao pode","provider":"openai"}`)
	c.Params = gin.Params{{Key: "id", Value: uuid.New().String()}}
	handler.Update(c)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", recorder.Code, recorder.Body.String())
	}
	if stub.updateCalls != 0 {
		t.Error("the stored installation credential was edited without the privilege")
	}
}

func TestUpdateOfAccountCredentialStaysOpen(t *testing.T) {
	withPermission(t, false, nil)
	stub := &scopeStubService{stored: &model.ApiKey{Scope: model.ScopeAccount}}
	handler := newScopeHandler(stub)

	c, recorder := requestWithToken(http.MethodPut,
		`{"name":"Da conta","provider":"openai"}`)
	c.Params = gin.Params{{Key: "id", Value: uuid.New().String()}}
	handler.Update(c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if stub.updateCalls != 1 {
		t.Errorf("update calls = %d, want 1", stub.updateCalls)
	}
}

func TestDeleteOfInstallationCredentialRefusedWithoutManage(t *testing.T) {
	withPermission(t, false, nil)
	stub := &scopeStubService{stored: &model.ApiKey{Scope: model.ScopeInstallation}}
	handler := newScopeHandler(stub)

	c, recorder := requestWithToken(http.MethodDelete, "")
	c.Params = gin.Params{{Key: "id", Value: uuid.New().String()}}
	handler.Delete(c)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", recorder.Code, recorder.Body.String())
	}
	if stub.deleteCalls != 0 {
		t.Error("the installation credential was deleted without the privilege")
	}
}

// An auth service that cannot answer is a denial: a credential every account
// inherits is not something to grant on a network error.
func TestInstallationScopeDeniedWhenPermissionCheckFails(t *testing.T) {
	withPermission(t, true, context.DeadlineExceeded)
	stub := &scopeStubService{}
	handler := newScopeHandler(stub)

	c, recorder := requestWithToken(http.MethodPost,
		`{"name":"Chave da casa","provider":"openai","key_value":"sk-abcdef","scope":"installation"}`)
	handler.Create(c)

	if recorder.Code == http.StatusCreated {
		t.Fatal("a failing permission check let the installation write through")
	}
	if stub.createCalls != 0 {
		t.Error("service was reached despite the failed permission check")
	}
}

// A target the gate cannot read demands the privilege instead of waiving it.
func TestUpdateDemandsTheGateWhenTheTargetCannotBeRead(t *testing.T) {
	permissions := withPermission(t, false, nil)
	stub := &scopeStubService{getErr: errors.New("connection reset by peer")}
	handler := newScopeHandler(stub)

	c, recorder := requestWithToken(http.MethodPut, `{"name":"Qualquer","provider":"openai"}`)
	c.Params = gin.Params{{Key: "id", Value: uuid.New().String()}}
	handler.Update(c)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", recorder.Code, recorder.Body.String())
	}
	if stub.updateCalls != 0 {
		t.Error("the write ran while the gate could not tell what it was guarding")
	}
	if len(permissions.asked) == 0 {
		t.Error("the gate was never consulted on an unreadable target")
	}
}

func TestDeleteDemandsTheGateWhenTheTargetCannotBeRead(t *testing.T) {
	withPermission(t, false, nil)
	stub := &scopeStubService{getErr: errors.New("connection reset by peer")}
	handler := newScopeHandler(stub)

	c, recorder := requestWithToken(http.MethodDelete, "")
	c.Params = gin.Params{{Key: "id", Value: uuid.New().String()}}
	handler.Delete(c)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", recorder.Code, recorder.Body.String())
	}
	if stub.deleteCalls != 0 {
		t.Error("the credential was deleted while the gate could not read it")
	}
}

// The grant still passes: failing closed must not mean failing always.
func TestUnreadableTargetPassesWithManage(t *testing.T) {
	withPermission(t, true, nil)
	stub := &scopeStubService{getErr: errors.New("connection reset by peer")}
	handler := newScopeHandler(stub)

	c, recorder := requestWithToken(http.MethodDelete, "")
	c.Params = gin.Params{{Key: "id", Value: uuid.New().String()}}
	handler.Delete(c)

	if recorder.Code == http.StatusForbidden {
		t.Fatalf("installation_configs.manage was granted and still got 403: %s", recorder.Body.String())
	}
	if stub.deleteCalls != 1 {
		t.Errorf("delete calls = %d, want 1", stub.deleteCalls)
	}
}
