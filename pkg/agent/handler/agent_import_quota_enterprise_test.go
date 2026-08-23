//go:build enterprise

package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	apierrors "evo-ai-core-service/internal/httpclient/errors"

	"github.com/gin-gonic/gin"
)

// CRM-116 — the agent-creating routes must ASK the quota, and ask it for the
// SIZE of the request. The unit tests on evaluate() prove the decision is right;
// only these prove the handler asks, which is what regressed. They swap
// checkAgentQuota to observe the call, so each fails if its call site is deleted.

type quotaCall struct {
	called     bool
	additional int
}

// spyQuota replaces the seam and restores it when the test ends.
func spyQuota(t *testing.T, answer error) *quotaCall {
	t.Helper()
	record := &quotaCall{}
	previous := checkAgentQuota
	checkAgentQuota = func(_ context.Context, additional int) error {
		record.called = true
		record.additional = additional
		return answer
	}
	t.Cleanup(func() { checkAgentQuota = previous })
	return record
}

func importRequest(t *testing.T, body []byte) (*httptest.ResponseRecorder, *gin.Context) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	buffer := &bytes.Buffer{}
	writer := multipart.NewWriter(buffer)
	part, err := writer.CreateFormFile("file", "agents.json")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(body); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/agents/import", buffer)
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	return recorder, c
}

func agentsJSON(t *testing.T, n int) []byte {
	t.Helper()
	agents := make([]map[string]interface{}, 0, n)
	for i := 0; i < n; i++ {
		agents = append(agents, map[string]interface{}{"name": "a", "type": "llm"})
	}
	payload, err := json.Marshal(agents)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return payload
}

// runImport drives the handler with a nil service; the run stops at the first
// call past the gate, which is enough to observe the gate itself.
func runImport(c *gin.Context) {
	defer func() { _ = recover() }()
	(&agentHandler{}).ImportAgents(c)
}

// THE regression. Fails if the gate is removed from ImportAgents.
func TestImportAgents_AsksTheQuotaForTheWholePayload(t *testing.T) {
	spy := spyQuota(t, nil)
	_, c := importRequest(t, agentsJSON(t, 500))

	runImport(c)

	if !spy.called {
		t.Fatal("ImportAgents did not ask the quota: a capped tenant can import any " +
			"number of agents (CRM-116)")
	}
	if spy.additional != 500 {
		t.Fatalf("quota asked for %d agents, want 500 — asking for 1 lets a bulk "+
			"import past a limit it should hit", spy.additional)
	}
}

// When the quota refuses, the import must stop — not merely report and proceed.
func TestImportAgents_RefusalStopsTheImport(t *testing.T) {
	rejection := apierrors.New("QUOTA_EXCEEDED", "agents limit reached (0/2, requested 500)",
		http.StatusUnprocessableEntity)
	spyQuota(t, rejection)
	recorder, c := importRequest(t, agentsJSON(t, 500))

	// A nil service would panic if the handler carried on; reaching the assertions
	// with a 422 is itself evidence that it did not.
	runImport(c)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("answered %d, want 422 when the quota refuses", recorder.Code)
	}
	if body := recorder.Body.String(); !bytes.Contains([]byte(body), []byte("QUOTA_EXCEEDED")) {
		t.Errorf("body %q does not carry QUOTA_EXCEEDED — the refusal must name the "+
			"gem's code, not answer a bare 422", body)
	}
}

// A malformed body is refused BEFORE the quota: asking how many agents an
// unparseable file creates is meaningless, and the caller deserves the parse
// error rather than a quota error.
func TestImportAgents_MalformedJSONNeverReachesTheQuota(t *testing.T) {
	spy := spyQuota(t, nil)
	recorder, c := importRequest(t, []byte("{not json"))

	runImport(c)

	if spy.called {
		t.Error("the quota was consulted for an unparseable payload")
	}
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("answered %d, want 400", recorder.Code)
	}
}

// An empty payload creates nothing, so it must not be charged: a tenant already
// at its limit would otherwise be refused for a request that writes no row.
func TestImportAgents_EmptyPayloadIsNotCharged(t *testing.T) {
	spy := spyQuota(t, nil)
	_, c := importRequest(t, []byte("[]"))

	runImport(c)

	if spy.called {
		t.Errorf("the quota was charged %d agents for an empty import", spy.additional)
	}
}

// The single-agent path must keep asking for exactly 1 — the signature change
// must not have quietly altered what Create requests.
func TestCreate_AsksTheQuotaForOne(t *testing.T) {
	spy := spyQuota(t, nil)
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/agents",
		bytes.NewBufferString(`{"name":"a","type":"llm"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	func() {
		defer func() { _ = recover() }()
		(&agentHandler{}).Create(c)
	}()

	if !spy.called {
		t.Fatal("Create did not ask the quota")
	}
	if spy.additional != 1 {
		t.Fatalf("Create asked for %d, want 1", spy.additional)
	}
}
