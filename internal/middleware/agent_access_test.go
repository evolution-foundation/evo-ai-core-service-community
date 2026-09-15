package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apiErrors "evo-ai-core-service/internal/httpclient/errors"
	"evo-ai-core-service/internal/infra/postgres"
	agentModel "evo-ai-core-service/pkg/agent/model"
	agentService "evo-ai-core-service/pkg/agent/service"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type lookupAgentService struct {
	agentService.AgentService
	err error
}

func (s lookupAgentService) GetByID(_ context.Context, id uuid.UUID) (*agentModel.Agent, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &agentModel.Agent{ID: id}, nil
}

func hitAgentRoute(t *testing.T, lookupErr error, id string) (*httptest.ResponseRecorder, bool) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	reached := false
	router := gin.New()
	router.PUT("/agents/:id",
		NewAgentAccessMiddleware(lookupAgentService{err: lookupErr}).GetAgentAccessMiddleware(),
		func(c *gin.Context) {
			reached = true
			c.Status(http.StatusNoContent)
		},
	)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/agents/"+id, strings.NewReader("{}")))
	return recorder, reached
}

func errorCode(t *testing.T, recorder *httptest.ResponseRecorder) (string, string) {
	t.Helper()
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not the error envelope: %s", recorder.Body.String())
	}
	return body.Error.Code, body.Error.Message
}

func TestAgentAccess_AgentThatDoesNotExistIs404(t *testing.T) {
	notFound := postgres.MapDBError(gorm.ErrRecordNotFound, agentModel.AgentErrors)

	recorder, reached := hitAgentRoute(t, notFound, uuid.New().String())

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d (%s), want 404", recorder.Code, recorder.Body.String())
	}
	if code, _ := errorCode(t, recorder); code != apiErrors.AgentNotFound {
		t.Errorf("code = %q, want %q", code, apiErrors.AgentNotFound)
	}
	if reached {
		t.Error("the handler ran for an agent that does not exist")
	}
}

func TestAgentAccess_LookupOutageIsAServerErrorWithoutTheDriverText(t *testing.T) {
	outage := postgres.MapDBError(errors.New("failed to connect to `host=db.internal`"), agentModel.AgentErrors)

	recorder, reached := hitAgentRoute(t, outage, uuid.New().String())

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d (%s), want 500", recorder.Code, recorder.Body.String())
	}
	if _, message := errorCode(t, recorder); strings.Contains(message, "db.internal") {
		t.Errorf("the driver error was echoed back: %q", message)
	}
	if reached {
		t.Error("the handler ran despite the failed lookup")
	}
}

func TestAgentAccess_MalformedIDStays400(t *testing.T) {
	recorder, reached := hitAgentRoute(t, nil, "not-a-uuid")

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	if reached {
		t.Error("the handler ran for a malformed id")
	}
}

func TestAgentAccess_ExistingAgentReachesTheHandler(t *testing.T) {
	recorder, reached := hitAgentRoute(t, nil, uuid.New().String())

	if !reached || recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, reached = %v", recorder.Code, reached)
	}
}
