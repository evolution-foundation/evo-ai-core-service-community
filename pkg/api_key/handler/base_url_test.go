package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"evo-ai-core-service/pkg/api_key/model"
	"evo-ai-core-service/pkg/api_key/service"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// callWithParams drives a handler that reads a path param, which `call` does
// not set.
func callWithParams(t *testing.T, method, target, body string, params gin.Params, run func(ApiKeyHandler, *gin.Context), svc service.ApiKeyService) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(method, target, bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = params

	run(newTestHandler(svc), c)
	return rec
}

// The endpoint the screen sends must survive the whole path, from request to
// persisted struct: a field that stops being carried fails these.

func TestCreatePersistsBaseURL(t *testing.T) {
	withPermission(t, true, nil)
	svc := &stubService{}

	call(t, http.MethodPost, "/agents/apikeys",
		`{"name":"Azure","provider":"azure","key_value":"sk-0001","base_url":"https://meu-gateway.example.com/v1"}`,
		func(h ApiKeyHandler, c *gin.Context) { h.Create(c) }, svc)

	if svc.created.BaseURL == nil {
		t.Fatal("base_url was discarded: the field never reached the persisted credential")
	}
	if *svc.created.BaseURL != "https://meu-gateway.example.com/v1" {
		t.Errorf("base_url = %q, want the value the request carried", *svc.created.BaseURL)
	}
}

// Absence means "the provider default", which is what every credential
// registered before the column meant.
func TestCreateWithoutBaseURLStoresNil(t *testing.T) {
	withPermission(t, true, nil)
	svc := &stubService{}

	call(t, http.MethodPost, "/agents/apikeys",
		`{"name":"OpenAI","provider":"openai","key_value":"sk-0001"}`,
		func(h ApiKeyHandler, c *gin.Context) { h.Create(c) }, svc)

	if svc.created.BaseURL != nil {
		t.Errorf("base_url = %q, want nil for an absent endpoint", *svc.created.BaseURL)
	}
}

// Blank and whitespace are "no endpoint", never a stored empty string: two
// spellings of the same state would resolve differently downstream.
func TestCreateTreatsBlankBaseURLAsAbsent(t *testing.T) {
	withPermission(t, true, nil)
	svc := &stubService{}

	call(t, http.MethodPost, "/agents/apikeys",
		`{"name":"OpenAI","provider":"openai","key_value":"sk-0001","base_url":"   "}`,
		func(h ApiKeyHandler, c *gin.Context) { h.Create(c) }, svc)

	if svc.created.BaseURL != nil {
		t.Errorf("base_url = %q, want nil for a blank endpoint", *svc.created.BaseURL)
	}
}

// The endpoint is not a secret: the screen has to render where the credential
// points, so it comes back in the response.
func TestResponseCarriesBaseURL(t *testing.T) {
	withPermission(t, true, nil)
	svc := &stubService{}

	recorder := call(t, http.MethodPost, "/agents/apikeys",
		`{"name":"Azure","provider":"azure","key_value":"sk-0001","base_url":"https://meu-gateway.example.com/v1"}`,
		func(h ApiKeyHandler, c *gin.Context) { h.Create(c) }, svc)

	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if envelope.Data["base_url"] != "https://meu-gateway.example.com/v1" {
		t.Errorf("response base_url = %v, want the stored endpoint", envelope.Data["base_url"])
	}
}

func TestUpdateWithoutBaseURLKeepsTheStoredOne(t *testing.T) {
	withPermission(t, true, nil)
	svc := &stubService{}

	callWithParams(t, http.MethodPut, "/agents/apikeys/x",
		`{"name":"Azure","provider":"azure"}`,
		gin.Params{{Key: "id", Value: uuid.New().String()}},
		func(h ApiKeyHandler, c *gin.Context) { h.Update(c) }, svc)

	if svc.updated == nil {
		t.Fatal("service was never called")
	}
	// GORM skips a nil pointer, which is what leaves the column alone.
	if svc.updated.BaseURL != nil {
		t.Errorf("an omitted base_url carried %q, which would overwrite the stored endpoint", *svc.updated.BaseURL)
	}
	if svc.updated.BaseURLSet {
		t.Error("an omitted base_url must not signal an explicit write")
	}
}

func TestUpdateWithBaseURLReplacesIt(t *testing.T) {
	withPermission(t, true, nil)
	svc := &stubService{}

	callWithParams(t, http.MethodPut, "/agents/apikeys/x",
		`{"name":"Azure","provider":"azure","base_url":"https://outro.example.com/v1"}`,
		gin.Params{{Key: "id", Value: uuid.New().String()}},
		func(h ApiKeyHandler, c *gin.Context) { h.Update(c) }, svc)

	if svc.updated.BaseURL == nil || *svc.updated.BaseURL != "https://outro.example.com/v1" {
		t.Errorf("base_url did not travel on update: %v", svc.updated.BaseURL)
	}
}

// Clearing back to the provider default is an explicit intent, and it needs the
// flag: a nil pointer alone would be skipped by GORM, making the clear a silent
// no-op exactly like the deactivation toggle used to be.
func TestUpdateClearingBaseURLSignalsAnExplicitWrite(t *testing.T) {
	withPermission(t, true, nil)
	svc := &stubService{}

	callWithParams(t, http.MethodPut, "/agents/apikeys/x",
		`{"name":"Azure","provider":"azure","base_url":""}`,
		gin.Params{{Key: "id", Value: uuid.New().String()}},
		func(h ApiKeyHandler, c *gin.Context) { h.Update(c) }, svc)

	if svc.updated.BaseURL != nil {
		t.Errorf("base_url = %q, want nil after an explicit clear", *svc.updated.BaseURL)
	}
	if !svc.updated.BaseURLSet {
		t.Error("clearing base_url did not signal an explicit write: the repository would skip it")
	}
}

func TestNormalizeBaseURLTrims(t *testing.T) {
	got := model.NormalizeBaseURL("  https://x.example.com/v1  ")
	if got == nil || *got != "https://x.example.com/v1" {
		t.Errorf("NormalizeBaseURL did not trim: %v", got)
	}
}
