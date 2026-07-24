package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"evo-ai-core-service/internal/httpclient/errors"
	"evo-ai-core-service/pkg/custom_mcp_server/model"
	"evo-ai-core-service/pkg/custom_mcp_server/service"

	"github.com/gin-gonic/gin"
)

// EVO-1739: the test-before-save handler is the only entry point that accepts a
// caller-supplied url, so its binding and its pass-through of the service error must be
// covered. The handler package had no tests at all before this.

// testConnectionStub records what the handler forwarded and replays a canned outcome.
// The service interface is embedded as a nil value so the stub satisfies it while only
// implementing the one method this handler calls — any other call panics loudly, which
// is the correct outcome for a test that should never make one.
type testConnectionStub struct {
	service.CustomMcpServerService

	gotURL     string
	gotHeaders map[string]string
	result     *model.TestResult
	err        error
}

func (s *testConnectionStub) TestConnection(_ context.Context, url string, headers map[string]string) (*model.TestResult, error) {
	s.gotURL = url
	s.gotHeaders = headers
	return s.result, s.err
}

func newTestConnectionRouter(stub *testConnectionStub) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := &customMcpServerHandler{customMcpServerService: stub}
	r := gin.New()
	r.POST("/custom-mcp-servers/test-connection", h.TestConnection)
	return r
}

func doTestConnection(t *testing.T, stub *testConnectionStub, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/custom-mcp-servers/test-connection", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	newTestConnectionRouter(stub).ServeHTTP(w, req)
	return w
}

func TestTestConnectionHandler_ForwardsURLAndHeaders(t *testing.T) {
	stub := &testConnectionStub{
		result: &model.TestResult{Success: true, StatusCode: http.StatusOK, ToolsCount: 4},
	}

	w := doTestConnection(t, stub, `{"url":"https://mcp.example/mcp","headers":{"Authorization":"Bearer sk-live"}}`)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d want %d (body %s)", w.Code, http.StatusOK, w.Body.String())
	}
	if stub.gotURL != "https://mcp.example/mcp" {
		t.Fatalf("url: got %q", stub.gotURL)
	}
	if stub.gotHeaders["Authorization"] != "Bearer sk-live" {
		t.Fatalf("headers: got %v", stub.gotHeaders)
	}

	var envelope struct {
		Data struct {
			TestResult model.TestResult `json:"test_result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode: %v (body %s)", err, w.Body.String())
	}
	// The frontend reads `data.test_result.tools_count` — pin the shape.
	if !envelope.Data.TestResult.Success || envelope.Data.TestResult.ToolsCount != 4 {
		t.Fatalf("test_result: got %+v", envelope.Data.TestResult)
	}
}

func TestTestConnectionHandler_RejectsMissingURL(t *testing.T) {
	stub := &testConnectionStub{result: &model.TestResult{Success: true}}

	w := doTestConnection(t, stub, `{"headers":{"X-Key":"v"}}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want %d (body %s)", w.Code, http.StatusBadRequest, w.Body.String())
	}
	if stub.gotURL != "" {
		t.Fatalf("service must not be called on an invalid body, got url %q", stub.gotURL)
	}
}

func TestTestConnectionHandler_RejectsNonStringHeaderValues(t *testing.T) {
	stub := &testConnectionStub{result: &model.TestResult{Success: true}}

	// The wizard's advanced-JSON mode can produce this; it must fail as a 400 rather
	// than reaching the service.
	w := doTestConnection(t, stub, `{"url":"https://mcp.example/mcp","headers":{"X-Api-Key":123}}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want %d (body %s)", w.Code, http.StatusBadRequest, w.Body.String())
	}
	if stub.gotURL != "" {
		t.Fatalf("service must not be called, got url %q", stub.gotURL)
	}
}

func TestTestConnectionHandler_PropagatesServiceValidationError(t *testing.T) {
	stub := &testConnectionStub{
		err: errors.New(errors.ValidationError, "url must use the http or https scheme", http.StatusBadRequest),
	}

	w := doTestConnection(t, stub, `{"url":"file:///etc/passwd"}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want %d (body %s)", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

// A failed handshake is still a successful call: the failure lives inside test_result,
// so the wizard can render "connection failed" instead of a network error.
func TestTestConnectionHandler_FailedHandshakeIsStill200(t *testing.T) {
	stub := &testConnectionStub{
		result: &model.TestResult{Success: false, StatusCode: http.StatusBadGateway, Error: "connection refused"},
	}

	w := doTestConnection(t, stub, `{"url":"https://mcp.example/mcp"}`)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d want %d (body %s)", w.Code, http.StatusOK, w.Body.String())
	}
	var envelope struct {
		Data struct {
			TestResult model.TestResult `json:"test_result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if envelope.Data.TestResult.Success || envelope.Data.TestResult.Error != "connection refused" {
		t.Fatalf("test_result: got %+v", envelope.Data.TestResult)
	}
}
