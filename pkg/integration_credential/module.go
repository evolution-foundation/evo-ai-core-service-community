package integrationCredential

import (
	"evo-ai-core-service/pkg/integration_credential/handler"
	"evo-ai-core-service/pkg/integration_credential/repository"
	"evo-ai-core-service/pkg/integration_credential/service"

	"gorm.io/gorm"
)

type Module struct {
	Handler handler.IntegrationCredentialHandler
	Service service.IntegrationCredentialService
	Repo    repository.IntegrationCredentialRepository
}

func New(db *gorm.DB, encryptionKey string) *Module {
	r := repository.NewIntegrationCredentialRepository(db)
	s := service.NewIntegrationCredentialService(r)
	h := handler.NewIntegrationCredentialHandler(s, encryptionKey)

	return &Module{
		Handler: h,
		Service: s,
		Repo:    r,
	}
}
