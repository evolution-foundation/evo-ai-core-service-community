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
	// The oauth sync reconciles reference rows on listing: the OAuth callbacks
	// live in the processor and stay untouched.
	oauthRepo := repository.NewOAuthConnectionRepository(db)
	migrationState := service.NewMigrationState(repository.NewMigrationStateRepository(db))
	h := handler.NewIntegrationCredentialHandler(s, encryptionKey, service.NewOAuthSync(oauthRepo, oauthRepo), migrationState)

	return &Module{
		Handler: h,
		Service: s,
		Repo:    r,
	}
}
