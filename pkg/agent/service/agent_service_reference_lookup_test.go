package service

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	apierrors "evo-ai-core-service/internal/httpclient/errors"
	errorsPostgres "evo-ai-core-service/internal/infra/postgres"
	"evo-ai-core-service/pkg/agent/model"
	apiKeyModel "evo-ai-core-service/pkg/api_key/model"
	apiKeyService "evo-ai-core-service/pkg/api_key/service"
	folderModel "evo-ai-core-service/pkg/folder/model"
	folderService "evo-ai-core-service/pkg/folder/service"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

const lookupDriverText = "failed to connect to `host=db.internal user=evo_app database=evo_community`"

type lookupFolderService struct {
	folderService.FolderService
	err error
}

func (f lookupFolderService) GetByID(_ context.Context, id uuid.UUID) (*folderModel.Folder, error) {
	if f.err != nil {
		return nil, errorsPostgres.MapDBError(f.err, folderModel.FolderErrors)
	}
	return &folderModel.Folder{ID: id}, nil
}

type lookupApiKeyService struct {
	apiKeyService.ApiKeyService
	err error
}

func (f lookupApiKeyService) GetByID(_ context.Context, id uuid.UUID) (*apiKeyModel.ApiKey, error) {
	if f.err != nil {
		return nil, errorsPostgres.MapDBError(f.err, apiKeyModel.APIKeyErrors)
	}
	return &apiKeyModel.ApiKey{ID: id}, nil
}

func serviceForReferenceLookups(repo *cardURLFakeRepo, folderErr, apiKeyErr error) *agentService {
	svc := serviceForProcessErrors(repo, nil)
	svc.folderService = lookupFolderService{err: folderErr}
	svc.apiKeyService = lookupApiKeyService{err: apiKeyErr}
	return svc
}

func llmAgent() model.Agent {
	return model.Agent{Name: "llm", Type: model.AgentTypeLLM, Model: "openai/gpt-4.1-mini", Config: "{}"}
}

func storedLLMAgent() *model.Agent {
	agent := llmAgent()
	agent.ID = uuid.New()
	return &agent
}

type referenceCase struct {
	name      string
	folderErr error
	apiKeyErr error
	apply     func(*model.Agent)
	field     string
	message   string
}

func missingReferenceCases() []referenceCase {
	return []referenceCase{
		{
			name:      "folder_id",
			folderErr: gorm.ErrRecordNotFound,
			apply:     func(a *model.Agent) { id := uuid.New(); a.FolderID = &id },
			field:     "folder_id",
			message:   "Folder not found",
		},
		{
			name:      "api_key_id",
			apiKeyErr: gorm.ErrRecordNotFound,
			apply:     func(a *model.Agent) { id := uuid.New(); a.ApiKeyID = &id },
			field:     "api_key_id",
			message:   "API key not found",
		},
	}
}

func TestCreate_MissingReferenceIs400NamingTheFieldAndPersistsNothing(t *testing.T) {
	for _, tc := range missingReferenceCases() {
		t.Run(tc.name, func(t *testing.T) {
			repo := &cardURLFakeRepo{}
			svc := serviceForReferenceLookups(repo, tc.folderErr, tc.apiKeyErr)
			agent := llmAgent()
			tc.apply(&agent)

			_, err := svc.Create(context.Background(), agent)

			assertClientError(t, err, http.StatusBadRequest, apierrors.ValidationError, tc.field, tc.message)
			if repo.createCalled {
				t.Error("the rejected agent was written")
			}
		})
	}
}

func TestUpdate_MissingReferenceIs400NamingTheFieldAndTheRowIsIntact(t *testing.T) {
	for _, tc := range missingReferenceCases() {
		t.Run(tc.name, func(t *testing.T) {
			repo := &cardURLFakeRepo{}
			svc := serviceForReferenceLookups(repo, tc.folderErr, tc.apiKeyErr)
			existing := storedLLMAgent()
			repo.stored = existing
			edit := llmAgent()
			tc.apply(&edit)

			_, err := svc.Update(context.Background(), &edit, existing.ID)

			assertClientError(t, err, http.StatusBadRequest, apierrors.ValidationError, tc.field, tc.message)
			if repo.updateCalled {
				t.Error("the rejected edit was written")
			}
			if repo.stored.FolderID != nil || repo.stored.ApiKeyID != nil {
				t.Errorf("stored references changed: folder_id=%v api_key_id=%v", repo.stored.FolderID, repo.stored.ApiKeyID)
			}
		})
	}
}

func TestImportAgents_MissingFolderIs400NamingTheFieldAndImportsNothing(t *testing.T) {
	repo := &cardURLFakeRepo{}
	svc := serviceForReferenceLookups(repo, gorm.ErrRecordNotFound, nil)
	folderID := uuid.New()

	_, err := svc.ImportAgents(context.Background(), model.AgentImportRequest{
		FolderID:  &folderID,
		AgentData: []map[string]interface{}{{"name": "llm", "type": "llm", "model": "openai/gpt-4.1-mini"}},
	})

	assertClientError(t, err, http.StatusBadRequest, apierrors.ValidationError, "folder_id", "Folder not found")
	if repo.createCalled {
		t.Error("an agent was imported into a folder that does not exist")
	}
}

func TestImportAgents_EntryWithMissingApiKeyIs400NamingTheFieldAndImportsNothing(t *testing.T) {
	repo := &cardURLFakeRepo{}
	svc := serviceForReferenceLookups(repo, nil, gorm.ErrRecordNotFound)

	_, err := svc.ImportAgents(context.Background(), model.AgentImportRequest{
		AgentData: []map[string]interface{}{
			{"name": "llm_ok", "type": "llm", "model": "openai/gpt-4.1-mini"},
			{"name": "llm_key", "type": "llm", "model": "openai/gpt-4.1-mini", "api_key_id": uuid.New().String()},
		},
	})

	assertClientError(t, err, http.StatusBadRequest, apierrors.ValidationError, "api_key_id", "API key not found")
	if repo.createCalled {
		t.Error("an entry before the rejected one was written")
	}
}

func TestUpdate_AgentThatDoesNotExistIs404(t *testing.T) {
	repo := &agentLookupRepo{getErr: gorm.ErrRecordNotFound}
	svc := serviceForReferenceLookups(&repo.cardURLFakeRepo, nil, nil)
	svc.agentRepository = repo

	edit := llmAgent()
	_, err := svc.Update(context.Background(), &edit, uuid.New())

	assertClientError(t, err, http.StatusNotFound, apierrors.AgentNotFound)
	if repo.updateCalled {
		t.Error("an update was attempted for an agent that does not exist")
	}
}

func assertLookupOutage(t *testing.T, err error, written bool) {
	t.Helper()

	if err == nil {
		t.Fatal("the request was accepted")
	}
	_, message, status := apierrors.HandleError(err)
	if status != http.StatusInternalServerError {
		t.Errorf("status = %d (%q), want 500 for a lookup outage", status, message)
	}
	if strings.Contains(message, "db.internal") {
		t.Errorf("the driver error was echoed back: %q", message)
	}
	if written {
		t.Error("the agent was written despite the failed lookup")
	}
}

func TestCreate_ReferenceLookupOutageStaysAServerError(t *testing.T) {
	outage := &pgconn.PgError{Code: "08006", Message: lookupDriverText}

	for _, tc := range missingReferenceCases() {
		t.Run(tc.name, func(t *testing.T) {
			repo := &cardURLFakeRepo{}
			folderErr, apiKeyErr := error(nil), error(nil)
			if tc.folderErr != nil {
				folderErr = outage
			} else {
				apiKeyErr = outage
			}
			svc := serviceForReferenceLookups(repo, folderErr, apiKeyErr)
			agent := llmAgent()
			tc.apply(&agent)

			_, err := svc.Create(context.Background(), agent)

			assertLookupOutage(t, err, repo.createCalled)
		})
	}
}

func TestUpdate_AgentLookupOutageStaysAServerError(t *testing.T) {
	repo := &agentLookupRepo{getErr: errors.New(lookupDriverText)}
	svc := serviceForReferenceLookups(&repo.cardURLFakeRepo, nil, nil)
	svc.agentRepository = repo

	edit := llmAgent()
	_, err := svc.Update(context.Background(), &edit, uuid.New())

	assertLookupOutage(t, err, repo.updateCalled)
}

// Fails the read the way the GORM repository does; cardURLFakeRepo's miss is a plain error.
type agentLookupRepo struct {
	cardURLFakeRepo
	getErr    error
	updateErr error
}

func (f *agentLookupRepo) GetByID(ctx context.Context, id uuid.UUID) (*model.Agent, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.cardURLFakeRepo.GetByID(ctx, id)
}

func (f *agentLookupRepo) Update(ctx context.Context, agent *model.Agent, id uuid.UUID) (*model.Agent, error) {
	if f.updateErr != nil {
		f.updateCalled = true
		return nil, f.updateErr
	}
	return f.cardURLFakeRepo.Update(ctx, agent, id)
}

// The write is mapped like the one in AssignFolder, so a name the tenant already
// uses answers 409 instead of the blanket 500 the generic wrapper produced.
func TestUpdate_DuplicateNameIsAConflictWithoutTheDriverText(t *testing.T) {
	repo := &agentLookupRepo{updateErr: &pgconn.PgError{Code: "23505", Message: lookupDriverText}}
	svc := serviceForReferenceLookups(&repo.cardURLFakeRepo, nil, nil)
	svc.agentRepository = repo
	existing := storedLLMAgent()
	repo.stored = existing

	edit := llmAgent()
	_, err := svc.Update(context.Background(), &edit, existing.ID)

	assertClientError(t, err, http.StatusConflict, string(errorsPostgres.ERR_DUPLICATE_KEY_VIOLATION), "already exists")
	if _, message, _ := apierrors.HandleError(err); strings.Contains(message, "db.internal") {
		t.Errorf("the driver error was echoed back: %q", message)
	}
}

func TestAssignFolder_MissingFolderIs400NamingTheFieldAndTheRowIsIntact(t *testing.T) {
	repo := &agentLookupRepo{}
	svc := serviceForReferenceLookups(&repo.cardURLFakeRepo, gorm.ErrRecordNotFound, nil)
	svc.agentRepository = repo
	existing := storedLLMAgent()
	repo.stored = existing
	folderID := uuid.New()

	_, err := svc.AssignFolder(context.Background(), existing.ID, &model.Agent{FolderID: &folderID})

	assertClientError(t, err, http.StatusBadRequest, apierrors.ValidationError, "folder_id", "Folder not found")
	if repo.updateCalled {
		t.Error("the agent was moved into a folder that does not exist")
	}
}

func TestAssignFolder_AgentThatDoesNotExistIs404(t *testing.T) {
	repo := &agentLookupRepo{getErr: gorm.ErrRecordNotFound}
	svc := serviceForReferenceLookups(&repo.cardURLFakeRepo, nil, nil)
	svc.agentRepository = repo
	folderID := uuid.New()

	_, err := svc.AssignFolder(context.Background(), uuid.New(), &model.Agent{FolderID: &folderID})

	assertClientError(t, err, http.StatusNotFound, apierrors.AgentNotFound)
}

func TestAssignFolder_LookupOutageStaysAServerError(t *testing.T) {
	outage := &pgconn.PgError{Code: "08006", Message: lookupDriverText}

	t.Run("agent", func(t *testing.T) {
		repo := &agentLookupRepo{getErr: outage}
		svc := serviceForReferenceLookups(&repo.cardURLFakeRepo, nil, nil)
		svc.agentRepository = repo
		folderID := uuid.New()

		_, err := svc.AssignFolder(context.Background(), uuid.New(), &model.Agent{FolderID: &folderID})

		assertLookupOutage(t, err, repo.updateCalled)
	})

	t.Run("folder", func(t *testing.T) {
		repo := &agentLookupRepo{}
		svc := serviceForReferenceLookups(&repo.cardURLFakeRepo, outage, nil)
		svc.agentRepository = repo
		existing := storedLLMAgent()
		repo.stored = existing
		folderID := uuid.New()

		_, err := svc.AssignFolder(context.Background(), existing.ID, &model.Agent{FolderID: &folderID})

		assertLookupOutage(t, err, repo.updateCalled)
	})
}

func TestListAgentsByFolderID_MissingFolderIs404(t *testing.T) {
	svc := serviceForReferenceLookups(&cardURLFakeRepo{}, gorm.ErrRecordNotFound, nil)

	_, err := svc.ListAgentsByFolderID(context.Background(), uuid.New(), 1, 20)

	assertClientError(t, err, http.StatusNotFound, apierrors.FolderNotFound)
}

func TestListAgentsByFolderID_FolderLookupOutageStaysAServerError(t *testing.T) {
	outage := &pgconn.PgError{Code: "08006", Message: lookupDriverText}
	svc := serviceForReferenceLookups(&cardURLFakeRepo{}, outage, nil)

	_, err := svc.ListAgentsByFolderID(context.Background(), uuid.New(), 1, 20)

	assertLookupOutage(t, err, false)
}
