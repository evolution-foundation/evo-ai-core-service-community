package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"evo-ai-core-service/pkg/integration_credential/model"
	"evo-ai-core-service/pkg/integration_credential/service"

	"github.com/fernet/fernet-go"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// fernetTestKey is a valid 32-byte urlsafe-base64 Fernet key, used only here.
const fernetTestKey = "cw_0x689RpI-jtRR7oE8h_eQsKImvJapLeSbXpwF4e4="

// stubService records what the handler hands down, so tests can assert on the
// persisted ciphertext and hint without a database.
type stubService struct {
	service.IntegrationCredentialService
	created         model.IntegrationCredential
	updated         *model.IntegrationCredential
	updatedIsActive *bool
	listed          []model.IntegrationCredential
	listRequest     model.IntegrationCredentialListRequest
	createCalls     int
}

// GetByID answers for the scope gate (EVO-2250 review, ALTO 5), which reads
// the stored credential before allowing a write that touches the installation
// default. Embedding the interface alone left it nil and panicked.
func (s *stubService) GetByID(_ context.Context, _ uuid.UUID) (*model.IntegrationCredential, error) {
	return &model.IntegrationCredential{Scope: model.ScopeAccount}, nil
}

func (s *stubService) Create(_ context.Context, request model.IntegrationCredential) (*model.IntegrationCredential, error) {
	s.created = request
	s.createCalls++
	request.ID = uuid.New()
	return &request, nil
}

func (s *stubService) Update(_ context.Context, request *model.IntegrationCredential, isActive *bool, id uuid.UUID) (*model.IntegrationCredential, error) {
	s.updated = request
	s.updatedIsActive = isActive
	stored := *request
	stored.ID = id
	return &stored, nil
}

func (s *stubService) List(_ context.Context, request model.IntegrationCredentialListRequest) (*model.IntegrationCredentialListResponse, error) {
	s.listRequest = request
	items := make([]model.IntegrationCredentialResponse, len(s.listed))
	for i, credential := range s.listed {
		items[i] = *credential.ToResponse()
	}
	return &model.IntegrationCredentialListResponse{
		Items:      items,
		Page:       request.Page,
		PageSize:   request.PageSize,
		TotalItems: int64(len(items)),
	}, nil
}

func newTestHandler(t *testing.T) (*stubService, IntegrationCredentialHandler) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	// These tests are about encryption, hints and kinds, not authorization, so
	// they run with the installation privilege granted. The gate itself has its
	// own path tests in installation_scope_test.go.
	withPermission(t, true, nil)
	stub := &stubService{}
	// nil reconciler: these tests cover the static path, where the oauth sync
	// must stay out of the way entirely.
	return stub, NewIntegrationCredentialHandler(stub, fernetTestKey, nil, nil, nil)
}

func postJSON(t *testing.T, h func(*gin.Context), body string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/integration-credentials", bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")
	h(c)
	return recorder
}

func decrypt(t *testing.T, ciphertext string) string {
	t.Helper()
	key, err := fernet.DecodeKey(fernetTestKey)
	if err != nil {
		t.Fatalf("decode key: %v", err)
	}
	plain := fernet.VerifyAndDecrypt([]byte(ciphertext), 0, []*fernet.Key{key})
	if plain == nil {
		t.Fatalf("stored value is not decryptable with the shared key: %q", ciphertext)
	}
	return string(plain)
}

func TestCreateEncryptsValueAndDerivesHint(t *testing.T) {
	stub, handler := newTestHandler(t)
	const secret = "app-dify-abcdef9c1d"

	recorder := postJSON(t, handler.Create,
		`{"name":"Dify producao","provider":"dify","value":"`+secret+`"}`)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", recorder.Code, recorder.Body.String())
	}
	if stub.created.Value == secret {
		t.Fatal("value was stored in plaintext")
	}
	if got := decrypt(t, stub.created.Value); got != secret {
		t.Errorf("decrypted value = %q, want %q", got, secret)
	}
	if stub.created.ValueHint != "9c1d" {
		t.Errorf("value_hint = %q, want 9c1d", stub.created.ValueHint)
	}
	if stub.created.Kind != model.KindStatic {
		t.Errorf("kind = %q, want static by default", stub.created.Kind)
	}
	if stub.created.Scope != model.ScopeAccount {
		t.Errorf("scope = %q, want account by default", stub.created.Scope)
	}
}

// Negative proof: the response must not carry the secret in either form. A
// handler that returned the entity instead of ToResponse would ship the
// ciphertext to the browser, and the ciphertext is decryptable by anyone
// holding the shared key.
func TestCreateResponseNeverCarriesTheValue(t *testing.T) {
	stub, handler := newTestHandler(t)
	const secret = "app-dify-abcdef9c1d"

	recorder := postJSON(t, handler.Create,
		`{"name":"Dify producao","provider":"dify","value":"`+secret+`"}`)

	body := recorder.Body.String()
	if strings.Contains(body, secret) {
		t.Errorf("response leaks the plaintext value: %s", body)
	}
	if strings.Contains(body, stub.created.Value) {
		t.Errorf("response leaks the ciphertext: %s", body)
	}

	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if _, present := envelope.Data["value"]; present {
		t.Errorf("response carries a \"value\" field: %s", body)
	}
	if envelope.Data["value_hint"] != "9c1d" {
		t.Errorf("value_hint = %v, want 9c1d", envelope.Data["value_hint"])
	}
}

// Negative proof: kind=oauth must be refused in 2.1. Accepting it would let a
// caller create a row the vault has no semantics for yet, and Story 2.5 is what
// defines where such a row points and who keeps it in sync.
func TestCreateRejectsOAuthKind(t *testing.T) {
	stub, handler := newTestHandler(t)

	recorder := postJSON(t, handler.Create,
		`{"name":"GitHub","provider":"github","kind":"oauth","value":""}`)

	if recorder.Code == http.StatusCreated {
		t.Fatalf("oauth kind was accepted: %s", recorder.Body.String())
	}
	if recorder.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", recorder.Code)
	}
	if stub.createCalls != 0 {
		t.Error("handler reached the service with an oauth credential")
	}
	if !strings.Contains(strings.ToLower(recorder.Body.String()), "oauth") {
		t.Errorf("error message does not mention oauth: %s", recorder.Body.String())
	}
}

func TestCreateRejectsMissingValue(t *testing.T) {
	stub, handler := newTestHandler(t)

	recorder := postJSON(t, handler.Create, `{"name":"Sem valor","provider":"dify"}`)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400: %s", recorder.Code, recorder.Body.String())
	}
	if stub.createCalls != 0 {
		t.Error("handler reached the service without a value")
	}
}

func TestCreateCompositeHintsOnTheSensitiveComponent(t *testing.T) {
	stub, handler := newTestHandler(t)
	envelope := `{\"user\":\"admin\",\"password\":\"s3nha-f9b2\"}`

	recorder := postJSON(t, handler.Create,
		`{"name":"n8n basic","provider":"n8n","value_format":"composite","value":"`+envelope+`"}`)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", recorder.Code, recorder.Body.String())
	}
	if stub.created.ValueFormat != model.ValueFormatComposite {
		t.Errorf("value_format = %q, want composite", stub.created.ValueFormat)
	}
	if stub.created.ValueHint != "f9b2" {
		t.Errorf("value_hint = %q, want f9b2 (last 4 of the password)", stub.created.ValueHint)
	}
	if strings.Contains(stub.created.ValueHint, "admin") {
		t.Error("hint exposes the public component")
	}
}

func TestCreateRejectsCompositeWithoutSecretField(t *testing.T) {
	stub, handler := newTestHandler(t)

	recorder := postJSON(t, handler.Create,
		`{"name":"n8n torto","provider":"n8n","value_format":"composite","value":"{\"user\":\"admin\"}"}`)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400: %s", recorder.Code, recorder.Body.String())
	}
	if stub.createCalls != 0 {
		t.Error("handler stored a composite credential with no secret component")
	}
}

func TestCreateNormalizesUnknownScopeAndKind(t *testing.T) {
	stub, handler := newTestHandler(t)

	recorder := postJSON(t, handler.Create,
		`{"name":"Torto","provider":"dify","scope":"agency","kind":"garbage","value_format":"weird","value":"segredo-1234"}`)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", recorder.Code, recorder.Body.String())
	}
	if stub.created.Scope != model.ScopeAccount {
		t.Errorf("scope = %q, want account (unknown scope must not widen)", stub.created.Scope)
	}
	if stub.created.Kind != model.KindStatic {
		t.Errorf("kind = %q, want static", stub.created.Kind)
	}
	if stub.created.ValueFormat != model.ValueFormatScalar {
		t.Errorf("value_format = %q, want scalar", stub.created.ValueFormat)
	}
}

func TestCreateAcceptsInstallationScope(t *testing.T) {
	stub, handler := newTestHandler(t)

	recorder := postJSON(t, handler.Create,
		`{"name":"ElevenLabs da casa","provider":"elevenlabs","scope":"installation","value":"el-key-7a3c"}`)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", recorder.Code, recorder.Body.String())
	}
	if stub.created.Scope != model.ScopeInstallation {
		t.Errorf("scope = %q, want installation", stub.created.Scope)
	}
}

func TestUpdateWithoutValueKeepsTheStoredOne(t *testing.T) {
	stub, handler := newTestHandler(t)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Params = gin.Params{{Key: "id", Value: uuid.New().String()}}
	c.Request = httptest.NewRequest(http.MethodPut, "/integration-credentials/x",
		bytes.NewBufferString(`{"name":"Renomeada","provider":"dify"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.Update(c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if stub.updated == nil {
		t.Fatal("service was never called")
	}
	// GORM's Updates skips zero-valued fields, so leaving these empty is what
	// keeps the stored secret untouched.
	if stub.updated.Value != "" {
		t.Errorf("update carries a value (%q); it would overwrite the stored secret", stub.updated.Value)
	}
	if stub.updated.ValueHint != "" {
		t.Errorf("update carries a hint (%q); it would desync from the stored secret", stub.updated.ValueHint)
	}
}

// The screen's activate/deactivate toggle travels as is_active on the update.
// A pointer is what lets `false` through: with a plain bool the zero value is
// indistinguishable from "not sent", and deactivation becomes a silent no-op
// (the exact bug the adversarial review of 2026-07-29 found).
func TestUpdatePassesIsActiveThrough(t *testing.T) {
	stub, handler := newTestHandler(t)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Params = gin.Params{{Key: "id", Value: uuid.New().String()}}
	c.Request = httptest.NewRequest(http.MethodPut, "/integration-credentials/x",
		bytes.NewBufferString(`{"name":"Dify","provider":"dify","is_active":false}`))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.Update(c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if stub.updatedIsActive == nil {
		t.Fatal("is_active=false never reached the service: the deactivate toggle is a no-op")
	}
	if *stub.updatedIsActive {
		t.Error("is_active arrived as true, want false")
	}
}

func TestUpdateWithoutIsActiveKeepsTheStoredState(t *testing.T) {
	stub, handler := newTestHandler(t)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Params = gin.Params{{Key: "id", Value: uuid.New().String()}}
	c.Request = httptest.NewRequest(http.MethodPut, "/integration-credentials/x",
		bytes.NewBufferString(`{"name":"Dify","provider":"dify"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.Update(c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if stub.updatedIsActive != nil {
		t.Errorf("an omitted is_active must stay nil (keep stored state), got %v", *stub.updatedIsActive)
	}
}

func TestUpdateWithValueReencryptsAndRehints(t *testing.T) {
	stub, handler := newTestHandler(t)
	const secret = "app-dify-novo-5e7f"

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Params = gin.Params{{Key: "id", Value: uuid.New().String()}}
	c.Request = httptest.NewRequest(http.MethodPut, "/integration-credentials/x",
		bytes.NewBufferString(`{"name":"Dify","provider":"dify","value":"`+secret+`"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.Update(c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if got := decrypt(t, stub.updated.Value); got != secret {
		t.Errorf("decrypted value = %q, want %q", got, secret)
	}
	if stub.updated.ValueHint != "5e7f" {
		t.Errorf("value_hint = %q, want 5e7f", stub.updated.ValueHint)
	}
	if strings.Contains(recorder.Body.String(), secret) {
		t.Errorf("update response leaks the plaintext: %s", recorder.Body.String())
	}
}

func TestUpdateRejectsOAuthKind(t *testing.T) {
	stub, handler := newTestHandler(t)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Params = gin.Params{{Key: "id", Value: uuid.New().String()}}
	c.Request = httptest.NewRequest(http.MethodPut, "/integration-credentials/x",
		bytes.NewBufferString(`{"name":"GitHub","provider":"github","kind":"oauth"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.Update(c)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400: %s", recorder.Code, recorder.Body.String())
	}
	if stub.updated != nil {
		t.Error("handler reached the service with an oauth credential")
	}
}

func TestListNeverCarriesValues(t *testing.T) {
	stub, handler := newTestHandler(t)
	const ciphertext = "gAAAAAB-ciphertext-of-the-secret"
	stub.listed = []model.IntegrationCredential{
		{Name: "Dify", Provider: "dify", Kind: model.KindStatic, Value: ciphertext, ValueHint: "9c1d", Scope: model.ScopeAccount, IsActive: true},
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/integration-credentials?page=1&pageSize=20&scope=account", nil)

	handler.List(c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), ciphertext) {
		t.Errorf("list leaks the ciphertext: %s", recorder.Body.String())
	}
	if stub.listRequest.Scope != model.ScopeAccount {
		t.Errorf("scope filter = %q, want account", stub.listRequest.Scope)
	}
}

// stubReporter answers the migration-state contract without a database.
type stubReporter struct {
	retired map[string]bool
	err     error
}

func (s *stubReporter) Retired(_ context.Context) (map[string]bool, error) {
	return s.retired, s.err
}

func TestMigrationStateReportsPerConsumer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reporter := &stubReporter{retired: map[string]bool{"custom_tools": true, "knowledge_nexus": false}}
	handler := NewIntegrationCredentialHandler(&stubService{}, fernetTestKey, nil, reporter, nil)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/integration-credentials/migration-state", nil)

	handler.MigrationState(c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}

	var envelope struct {
		Data struct {
			Retired map[string]bool `json:"retired"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !envelope.Data.Retired["custom_tools"] {
		t.Error("custom_tools should be reported as retired")
	}
	if envelope.Data.Retired["knowledge_nexus"] {
		t.Error("knowledge_nexus should not be reported as retired")
	}
}

// Negative proof: a failed read must answer "nothing retired" rather than an
// error. The screens read a missing answer as not retired and keep the inline
// field editable; failing the request would leave the form unable to decide.
func TestMigrationStateFailureIsNeverRetired(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reporter := &stubReporter{retired: map[string]bool{"custom_tools": false}, err: context.DeadlineExceeded}
	handler := NewIntegrationCredentialHandler(&stubService{}, fernetTestKey, nil, reporter, nil)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/integration-credentials/migration-state", nil)

	handler.MigrationState(c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 even on a read failure", recorder.Code)
	}
	if strings.Contains(recorder.Body.String(), `"custom_tools":true`) {
		t.Errorf("a failed read reported a consumer as retired: %s", recorder.Body.String())
	}
}

// The literal route must be registered BEFORE /:id, or gin captures it as an id
// and the endpoint answers "credential not found" instead of the state.
//
// Registration goes through the global permission middleware, which a unit test
// has no business booting, so the ordering is asserted on the source itself.
func TestMigrationStateRouteIsRegisteredBeforeTheIdRoute(t *testing.T) {
	source, err := os.ReadFile("integration_credential_handler.go")
	if err != nil {
		t.Fatalf("read handler source: %v", err)
	}

	body := string(source)
	literal := strings.Index(body, `credentials.GET("/migration-state"`)
	parameterized := strings.Index(body, `credentials.GET("/:id"`)

	if literal == -1 || parameterized == -1 {
		t.Fatal("expected both routes to be registered")
	}
	if literal > parameterized {
		t.Error(`"/migration-state" is registered after "/:id" and would be captured by it`)
	}
}

// stubReferences answers the AC10 aggregation without a database.
type stubReferences struct {
	index service.ReferenceIndex
	err   error
	calls int
}

func (s *stubReferences) Build(_ context.Context) (service.ReferenceIndex, error) {
	s.calls++
	return s.index, s.err
}

func TestListAttachesReferencedBy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &stubService{}
	id := uuid.New()
	stub.listed = []model.IntegrationCredential{{ID: id, Name: "Dify", Provider: "dify", Kind: model.KindStatic, IsActive: true}}
	references := &stubReferences{index: service.ReferenceIndex{id: {"Agente Dify", "Bot de canal (evo_ai)"}}}
	handler := NewIntegrationCredentialHandler(stub, fernetTestKey, nil, nil, references)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/integration-credentials?page=1&pageSize=20", nil)

	handler.List(c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "Agente Dify") || !strings.Contains(body, "Bot de canal") {
		t.Errorf("referenced_by is missing from the payload: %s", body)
	}
	// The whole page is aggregated in ONE pass, never per credential.
	if references.calls != 1 {
		t.Errorf("the stores were read %d times for one page, want 1", references.calls)
	}
}

// A credential nobody uses reports an empty ARRAY, not null: the screen tells
// "no consumers" apart from "the server does not report this".
func TestListReportsAnEmptyArrayForAnUnusedCredential(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &stubService{}
	stub.listed = []model.IntegrationCredential{{ID: uuid.New(), Name: "Sem uso", Provider: "dify", Kind: model.KindStatic, IsActive: true}}
	handler := NewIntegrationCredentialHandler(stub, fernetTestKey, nil, nil, &stubReferences{index: service.ReferenceIndex{}})

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/integration-credentials?page=1&pageSize=20", nil)

	handler.List(c)

	if !strings.Contains(recorder.Body.String(), `"referenced_by":[]`) {
		t.Errorf(`expected "referenced_by":[] in the payload: %s`, recorder.Body.String())
	}
}

// The aggregation is decoration: if it fails, the credentials still list.
func TestListStillRendersWhenTheAggregationFails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &stubService{}
	stub.listed = []model.IntegrationCredential{{ID: uuid.New(), Name: "Dify", Provider: "dify", Kind: model.KindStatic, IsActive: true}}
	handler := NewIntegrationCredentialHandler(stub, fernetTestKey, nil, nil, &stubReferences{err: context.DeadlineExceeded})

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/integration-credentials?page=1&pageSize=20", nil)

	handler.List(c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("a failed aggregation took the listing down: %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "Dify") {
		t.Errorf("the credential itself is missing: %s", recorder.Body.String())
	}
}
