package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"evo-ai-core-service/pkg/api_key/model"
	"evo-ai-core-service/pkg/api_key/service"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// fernetTestKey is a valid 32-byte urlsafe-base64 Fernet key, used only here.
const fernetTestKey = "cw_0x689RpI-jtRR7oE8h_eQsKImvJapLeSbXpwF4e4="

// stubService implements service.ApiKeyService, recording what the handler
// hands down so tests can assert on the persisted key and hint.
type stubService struct {
	service.ApiKeyService
	created         model.ApiKey
	updated         *model.ApiKey
	updatedIsActive *bool
	listed          []model.ApiKey
	listRequest     model.ApiKeyListRequest
}

func (s *stubService) Create(_ context.Context, request model.ApiKey) (*model.ApiKey, error) {
	s.created = request
	request.ID = uuid.New()
	return &request, nil
}

func (s *stubService) Update(_ context.Context, request *model.ApiKey, isActive *bool, id uuid.UUID) (*model.ApiKey, error) {
	s.updated = request
	s.updatedIsActive = isActive
	stored := *request
	stored.ID = id
	return &stored, nil
}

func (s *stubService) List(_ context.Context, request model.ApiKeyListRequest) (*model.ApiKeyListResponse, error) {
	s.listRequest = request
	items := make([]model.ApiKeyResponse, len(s.listed))
	for i, apiKey := range s.listed {
		items[i] = *apiKey.ToResponse()
	}
	return &model.ApiKeyListResponse{
		Items:      items,
		Page:       request.Page,
		PageSize:   request.PageSize,
		TotalItems: int64(len(items)),
	}, nil
}

func newTestHandler(svc service.ApiKeyService) ApiKeyHandler {
	return NewApiKeyHandler(svc, fernetTestKey)
}

func call(t *testing.T, method, target, body string, run func(ApiKeyHandler, *gin.Context), svc service.ApiKeyService) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	var reader *bytes.Buffer
	if body == "" {
		reader = bytes.NewBufferString("")
	} else {
		reader = bytes.NewBufferString(body)
	}
	c.Request = httptest.NewRequest(method, target, reader)
	c.Request.Header.Set("Content-Type", "application/json")

	run(newTestHandler(svc), c)
	return rec
}

func TestCreateStoresHintAndHidesKey(t *testing.T) {
	svc := &stubService{}
	const plainKey = "sk-proj-super-secret-4f2a"

	rec := call(t, http.MethodPost, "/agents/apikeys",
		`{"name":"Producao","provider":"openai","key_value":"`+plainKey+`"}`,
		func(h ApiKeyHandler, c *gin.Context) { h.Create(c) }, svc)

	if rec.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d: %s", rec.Code, rec.Body.String())
	}

	if svc.created.KeyHint != "4f2a" {
		t.Errorf("persisted key_hint = %q, want 4f2a", svc.created.KeyHint)
	}
	if svc.created.Key == plainKey {
		t.Error("key was persisted in plaintext")
	}
	if svc.created.Key == "" {
		t.Error("key was not persisted")
	}

	assertResponseHidesKey(t, rec.Body.String(), plainKey, svc.created.Key)
}

func TestUpdateWithoutKeyKeepsStoredCredential(t *testing.T) {
	svc := &stubService{}

	rec := call(t, http.MethodPut, "/agents/apikeys/"+uuid.NewString(),
		`{"name":"Producao renomeada","provider":"openai"}`,
		func(h ApiKeyHandler, c *gin.Context) {
			c.Params = gin.Params{{Key: "id", Value: uuid.NewString()}}
			h.Update(c)
		}, svc)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if svc.updated == nil {
		t.Fatal("service.Update was never called")
	}
	// Zero-valued Key/KeyHint make GORM skip both columns, preserving the stored key.
	if svc.updated.Key != "" {
		t.Errorf("Key = %q, want empty so the stored key survives", svc.updated.Key)
	}
	if svc.updated.KeyHint != "" {
		t.Errorf("KeyHint = %q, want empty so the stored hint survives", svc.updated.KeyHint)
	}
	if svc.updated.Name != "Producao renomeada" {
		t.Errorf("Name = %q, want the new name", svc.updated.Name)
	}
}

func TestUpdateWithKeyRefreshesHint(t *testing.T) {
	svc := &stubService{}
	const plainKey = "sk-proj-rotated-91bc"

	rec := call(t, http.MethodPut, "/agents/apikeys/"+uuid.NewString(),
		`{"name":"Producao","provider":"openai","key_value":"`+plainKey+`"}`,
		func(h ApiKeyHandler, c *gin.Context) {
			c.Params = gin.Params{{Key: "id", Value: uuid.NewString()}}
			h.Update(c)
		}, svc)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if svc.updated.KeyHint != "91bc" {
		t.Errorf("KeyHint = %q, want 91bc", svc.updated.KeyHint)
	}
	if svc.updated.Key == plainKey || svc.updated.Key == "" {
		t.Errorf("Key = %q, want a non-empty ciphertext", svc.updated.Key)
	}

	assertResponseHidesKey(t, rec.Body.String(), plainKey, svc.updated.Key)
}

func TestListNeverReturnsAnyKey(t *testing.T) {
	encrypted := "gAAAAABciphertext-that-must-not-reach-the-browser"
	svc := &stubService{listed: []model.ApiKey{
		{Name: "Producao", Provider: "openai", Key: encrypted, KeyHint: "4f2a", IsActive: true},
		{Name: "Testes", Provider: "anthropic", Key: encrypted, KeyHint: "91bc", IsActive: false},
	}}

	rec := call(t, http.MethodGet, "/agents/apikeys", "",
		func(h ApiKeyHandler, c *gin.Context) { h.List(c) }, svc)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	if strings.Contains(body, encrypted) {
		t.Errorf("list leaked the encrypted key: %s", body)
	}
	if !strings.Contains(body, `"key_hint":"4f2a"`) {
		t.Errorf("list is missing the key hint: %s", body)
	}
	// The OpenAI-compatible flag drives the "serves which features" column.
	if !strings.Contains(body, `"openai_compatible":true`) ||
		!strings.Contains(body, `"openai_compatible":false`) {
		t.Errorf("list is missing openai_compatible per provider: %s", body)
	}
}

// EVO-2250 story 1.2: credentials carry the scope they belong to. The
// resolution chain itself lives in the CRM — this service only stores it.
func TestCreateStoresRequestedScope(t *testing.T) {
	svc := &stubService{}

	call(t, http.MethodPost, "/agents/apikeys",
		`{"name":"Chave da casa","provider":"openai","key_value":"sk-house-0001","scope":"installation"}`,
		func(h ApiKeyHandler, c *gin.Context) { h.Create(c) }, svc)

	if svc.created.Scope != model.ScopeInstallation {
		t.Errorf("scope = %q, want %q", svc.created.Scope, model.ScopeInstallation)
	}
}

func TestCreateDefaultsToAccountScope(t *testing.T) {
	for _, body := range []string{
		`{"name":"A","provider":"openai","key_value":"sk-0001"}`,
		`{"name":"B","provider":"openai","key_value":"sk-0002","scope":""}`,
		`{"name":"C","provider":"openai","key_value":"sk-0003","scope":"nonsense"}`,
	} {
		svc := &stubService{}
		call(t, http.MethodPost, "/agents/apikeys", body,
			func(h ApiKeyHandler, c *gin.Context) { h.Create(c) }, svc)

		// An unknown scope must never widen a credential to the installation.
		if svc.created.Scope != model.ScopeAccount {
			t.Errorf("body %s → scope %q, want %q", body, svc.created.Scope, model.ScopeAccount)
		}
	}
}

func TestUpdateWithoutScopeKeepsStoredScope(t *testing.T) {
	svc := &stubService{}

	call(t, http.MethodPut, "/agents/apikeys/"+uuid.NewString(),
		`{"name":"Producao","provider":"openai"}`,
		func(h ApiKeyHandler, c *gin.Context) {
			c.Params = gin.Params{{Key: "id", Value: uuid.NewString()}}
			h.Update(c)
		}, svc)

	// Zero value makes GORM skip the column, preserving the stored scope.
	if svc.updated.Scope != "" {
		t.Errorf("scope = %q, want empty so the stored scope survives", svc.updated.Scope)
	}
}

func TestListForwardsScopeFilter(t *testing.T) {
	svc := &stubService{}

	call(t, http.MethodGet, "/agents/apikeys?scope=installation", "",
		func(h ApiKeyHandler, c *gin.Context) { h.List(c) }, svc)

	if svc.listRequest.Scope != model.ScopeInstallation {
		t.Errorf("list scope = %q, want %q", svc.listRequest.Scope, model.ScopeInstallation)
	}
}

func TestListWithoutScopeReturnsEveryScope(t *testing.T) {
	svc := &stubService{}

	call(t, http.MethodGet, "/agents/apikeys", "",
		func(h ApiKeyHandler, c *gin.Context) { h.List(c) }, svc)

	// The settings screen renders both sections from a single call.
	if svc.listRequest.Scope != "" {
		t.Errorf("list scope = %q, want empty (no filter)", svc.listRequest.Scope)
	}
}

func TestListResponseCarriesScope(t *testing.T) {
	svc := &stubService{listed: []model.ApiKey{
		{Name: "Chave da casa", Provider: "openai", Scope: model.ScopeInstallation, IsActive: true},
		{Name: "Producao", Provider: "openai", Scope: model.ScopeAccount, IsActive: true},
	}}

	rec := call(t, http.MethodGet, "/agents/apikeys", "",
		func(h ApiKeyHandler, c *gin.Context) { h.List(c) }, svc)

	body := rec.Body.String()
	if !strings.Contains(body, `"scope":"installation"`) ||
		!strings.Contains(body, `"scope":"account"`) {
		t.Errorf("list is missing the scope per credential: %s", body)
	}
}

// assertResponseHidesKey fails when a single-item envelope carries the key in
// any form, plaintext or ciphertext.
func assertResponseHidesKey(t *testing.T, body, plainKey, encryptedKey string) {
	t.Helper()

	if strings.Contains(body, plainKey) {
		t.Errorf("response leaked the plaintext key: %s", body)
	}
	if encryptedKey != "" && strings.Contains(body, encryptedKey) {
		t.Errorf("response leaked the encrypted key: %s", body)
	}

	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		t.Fatalf("unmarshal response: %v (%s)", err, body)
	}
	if _, present := envelope.Data["key"]; present {
		t.Errorf("response still carries a \"key\" field: %s", body)
	}
}
