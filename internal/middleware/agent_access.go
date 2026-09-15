package middleware

import (
	"net/http"

	apiErrors "evo-ai-core-service/internal/httpclient/errors"
	"evo-ai-core-service/internal/httpclient/response"
	"evo-ai-core-service/internal/infra/postgres"
	agentService "evo-ai-core-service/pkg/agent/service"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type AgentAccessMiddleware interface {
	GetAgentAccessMiddleware() gin.HandlerFunc
}

type agentAccessMiddleware struct {
	agentService agentService.AgentService
}

func NewAgentAccessMiddleware(agentService agentService.AgentService) AgentAccessMiddleware {
	return &agentAccessMiddleware{agentService: agentService}
}

func bodyToMap(c *gin.Context) (map[string]interface{}, error) {
	var body map[string]interface{}
	if err := c.ShouldBindJSON(&body); err != nil {
		return nil, err
	}

	return body, nil
}

func (a *agentAccessMiddleware) GetAgentAccessMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()

		var agentID uuid.UUID
		var errAgentID error

		switch c.Request.Method {
		case http.MethodPost:
			agentID, errAgentID = uuid.Parse(c.Param("id"))
		case http.MethodDelete, http.MethodPut:
			agentID, errAgentID = uuid.Parse(c.Param("id"))
		default:
			agentID, errAgentID = uuid.Parse(c.Param("id"))
		}

		if errAgentID != nil {
			response.ErrorResponse(c, apiErrors.BadRequest, "Invalid agent ID", nil, http.StatusBadRequest)
			c.Abort()
			return
		}

		_, err := a.agentService.GetByID(ctx, agentID)
		if err != nil {
			if postgres.IsRecordNotFound(err) {
				response.ErrorResponse(c, apiErrors.AgentNotFound, "Agent not found", nil, http.StatusNotFound)
			} else {
				response.ErrorResponse(c, apiErrors.InternalError, "Failed to get agent", nil, http.StatusInternalServerError)
			}
			c.Abort()
			return
		}

		c.Next()
	}
}
