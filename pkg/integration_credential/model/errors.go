package model

import "evo-ai-core-service/internal/infra/postgres"

var IntegrationCredentialErrors = []postgres.CustomErrorMessage{
	{
		Code: string(postgres.ERR_DUPLICATE_KEY_VIOLATION),
		// Uniqueness is (scope, name), so the same name may exist once per
		// scope. Saying only "already exists" would send the user hunting for
		// a credential that is not in the list they are looking at.
		Message: "An integration credential with this name already exists in this scope",
	},
	{
		Code:    string(postgres.ERR_RECORD_NOT_FOUND),
		Message: "Integration credential not found",
	},
	{
		Code: string(postgres.ERR_CHECK_CONSTRAINT_VIOLATION),
		// The coherence CHECK: a static credential carries a value, an oauth
		// one carries an owner reference and no value.
		Message: "Integration credential does not match its kind: static requires a value, oauth requires an owner reference and no value",
	},
}
