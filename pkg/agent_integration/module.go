package agent_integration

import (
	"evo-ai-core-service/pkg/agent_integration/handler"
	"evo-ai-core-service/pkg/agent_integration/repository"
	"evo-ai-core-service/pkg/agent_integration/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func InitModule(db *gorm.DB, router gin.IRouter) {
	// Initialize repository
	agentIntegrationRepository := repository.NewAgentIntegrationRepository(db)

	// Initialize service. The credential lookup validates a vault reference on
	// write, so an unusable credential_id fails for whoever is configuring the
	// agent instead of on the next conversation.
	agentIntegrationService := service.NewAgentIntegrationService(
		agentIntegrationRepository,
		repository.NewCredentialLookup(db),
	)

	// Initialize handler and register routes
	agentIntegrationHandler := handler.NewAgentIntegrationHandler(agentIntegrationService)
	agentIntegrationHandler.RegisterRoutesMiddleware(router)
}
