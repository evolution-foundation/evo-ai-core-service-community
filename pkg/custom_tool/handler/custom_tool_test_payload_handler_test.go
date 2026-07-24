package handler

import (
	"bytes"
	"context"
	"encoding/json"
	apiErrors "evo-ai-core-service/internal/httpclient/errors"
	"evo-ai-core-service/pkg/custom_tool/model"
	"evo-ai-core-service/pkg/custom_tool/service"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// stubService implements service.CustomToolService; only TestPayload is exercised.
type stubService struct {
	service.CustomToolService
	gotReq model.CustomToolTestPayloadRequest
	result *model.TestResult
	err    error
}

func (s *stubService) TestPayload(_ context.Context, req model.CustomToolTestPayloadRequest) (*model.TestResult, error) {
	s.gotReq = req
	return s.result, s.err
}

func postTestPayload(t *testing.T, svc service.CustomToolService, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/custom-tools/test", bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")

	NewCustomToolHandler(svc).TestPayload(c)
	return rec
}

// R1 review (EVO-1738): the handler package had no tests at all, so the new
// test-before-save endpoint's binding and response envelope were unverified.
func TestTestPayloadHandler_ForwardsFullRequestAndWrapsResult(t *testing.T) {
	svc := &stubService{result: &model.TestResult{Success: true, StatusCode: 200, Body: `{"ok":true}`}}

	rec := postTestPayload(t, svc, `{
		"method": "GET",
		"endpoint": "https://api.example.com/users/{user_id}",
		"headers": {"X-Token": "abc"},
		"path_params": {"user_id": "42"},
		"query_params": {"limit": 10},
		"body_params": {"ignored": true}
	}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// path_params/query_params must survive the bind — dropping them is what made
	// the wizard report "Request OK" for a request missing the user's config.
	if svc.gotReq.PathParams["user_id"] != "42" {
		t.Fatalf("path_params not bound: %#v", svc.gotReq.PathParams)
	}
	if svc.gotReq.QueryParams["limit"] != float64(10) {
		t.Fatalf("query_params not bound: %#v", svc.gotReq.QueryParams)
	}
	if svc.gotReq.Headers["X-Token"] != "abc" {
		t.Fatalf("headers not bound: %#v", svc.gotReq.Headers)
	}

	var envelope struct {
		Success bool `json:"success"`
		Data    struct {
			TestResult model.TestResult `json:"test_result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !envelope.Success || !envelope.Data.TestResult.Success || envelope.Data.TestResult.StatusCode != 200 {
		t.Fatalf("unexpected envelope: %s", rec.Body.String())
	}
}

func TestTestPayloadHandler_RejectsMissingRequiredFields(t *testing.T) {
	rec := postTestPayload(t, &stubService{}, `{"endpoint": "https://api.example.com"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400 when method is missing, got %d: %s", rec.Code, rec.Body.String())
	}
}

// An unsupported method is caller input: it must surface as 400 with the real
// reason, not a generic 500 the UI cannot explain.
func TestTestPayloadHandler_MapsServiceApiErrorStatus(t *testing.T) {
	svc := &stubService{err: apiErrors.New(apiErrors.InvalidInput, "unsupported method: TRACE", http.StatusBadRequest)}

	rec := postTestPayload(t, svc, `{"method": "TRACE", "endpoint": "https://api.example.com"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", rec.Code, rec.Body.String())
	}

	var envelope struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	// The frontend reads error.message — assert the reason actually lands there.
	if envelope.Error.Message != "unsupported method: TRACE" {
		t.Fatalf("reason not surfaced in error.message: %s", rec.Body.String())
	}
}
