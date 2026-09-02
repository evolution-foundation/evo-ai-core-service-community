package agent_integration

import (
	"evo-ai-core-service/pkg/agent_integration/handler"
	"evo-ai-core-service/pkg/agent_integration/repository"
	"evo-ai-core-service/pkg/agent_integration/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// encryptionKey is the shared Fernet key: the Knowledge Nexus space discovery
// resolves a vault credential server-side to build the upstream request.
func InitModule(db *gorm.DB, router gin.IRouter, encryptionKey string) {
	// Initialize repository
	agentIntegrationRepository := repository.NewAgentIntegrationRepository(db)

	// Initialize service. The credential lookup validates a vault reference on
	// write, so an unusable credential_id fails for whoever is configuring the
	// agent instead of on the next conversation.
	agentIntegrationService := service.NewAgentIntegrationService(
		agentIntegrationRepository,
		repository.NewCredentialLookup(db, encryptionKey),
	)

	// Initialize handler and register routes
	agentIntegrationHandler := handler.NewAgentIntegrationHandler(agentIntegrationService)
	agentIntegrationHandler.RegisterRoutesMiddleware(router)
}
