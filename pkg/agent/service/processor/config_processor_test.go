package processor

import (
	"context"
	"strings"
	"testing"

	mcpmodel "evo-ai-core-service/pkg/mcp_server/model"

	"github.com/google/uuid"
)

// An official MCP server takes a secret env var from the vault, referenced by
// name in `credential_refs`.
//// `processMCPServers` rebuilds each entry from an ALLOWLIST of keys, so a field
// missing from it is dropped in silence and the runtime downstream finds no
// reference to resolve.

const serverID = "11111111-1111-1111-1111-111111111111"

func processorWithCatalog(t *testing.T, requiredEnvs string) ConfigProcessor {
	t.Helper()
	return NewConfigProcessor(
		func() string { return "generated-key" },
		func(_ context.Context, _ uuid.UUID) (*mcpmodel.McpServer, error) {
			return &mcpmodel.McpServer{
				Name:         "GitHub",
				Environments: requiredEnvs,
			}, nil
		},
	)
}

func TestCredentialRefsSurviveProcessing(t *testing.T) {
	processor := processorWithCatalog(t, `{"GITHUB_PERSONAL_ACCESS_TOKEN":"env@@GITHUB_PERSONAL_ACCESS_TOKEN"}`)

	processed, err := processor.processMCPServers(context.Background(), []interface{}{
		map[string]interface{}{
			"id":              serverID,
			"environments":    map[string]interface{}{"GITHUB_PERSONAL_ACCESS_TOKEN": "ghp-inline"},
			"credential_refs": map[string]interface{}{"GITHUB_PERSONAL_ACCESS_TOKEN": "cred-uuid"},
			"tools":           []interface{}{"search"},
		},
	})
	if err != nil {
		t.Fatalf("processing failed: %v", err)
	}

	refs, ok := processed[0]["credential_refs"].(map[string]interface{})
	if !ok {
		t.Fatal("credential_refs was dropped: the runtime would never see a reference to resolve")
	}
	if refs["GITHUB_PERSONAL_ACCESS_TOKEN"] != "cred-uuid" {
		t.Errorf("credential_refs = %v, want the reference the request carried", refs)
	}
	// The inline value stays as the fallback until story 2.7 retires it.
	envs := processed[0]["environments"].(map[string]interface{})
	if envs["GITHUB_PERSONAL_ACCESS_TOKEN"] != "ghp-inline" {
		t.Errorf("inline env was lost: %v", envs)
	}
}

// A required key satisfied ONLY by a vault reference must pass validation:
// demanding a plaintext value would make the vault unusable for exactly the
// secrets it exists to hold.
func TestRequiredEnvSatisfiedByVaultReference(t *testing.T) {
	processor := processorWithCatalog(t, `{"GITHUB_PERSONAL_ACCESS_TOKEN":"env@@GITHUB_PERSONAL_ACCESS_TOKEN"}`)

	processed, err := processor.processMCPServers(context.Background(), []interface{}{
		map[string]interface{}{
			"id":              serverID,
			"environments":    map[string]interface{}{},
			"credential_refs": map[string]interface{}{"GITHUB_PERSONAL_ACCESS_TOKEN": "cred-uuid"},
			"tools":           []interface{}{},
		},
	})
	if err != nil {
		t.Fatalf("a vault-referenced env var was rejected as missing: %v", err)
	}
	if processed[0]["credential_refs"] == nil {
		t.Error("the reference did not survive the very call that accepted it")
	}
}

// The validation still bites when a required key has neither a value nor a
// reference: the vault must not become a way around it.
func TestRequiredEnvStillRejectedWhenNeitherProvided(t *testing.T) {
	processor := processorWithCatalog(t, `{"GITHUB_PERSONAL_ACCESS_TOKEN":"env@@GITHUB_PERSONAL_ACCESS_TOKEN"}`)

	_, err := processor.processMCPServers(context.Background(), []interface{}{
		map[string]interface{}{
			"id":              serverID,
			"environments":    map[string]interface{}{},
			"credential_refs": map[string]interface{}{"OUTRA_VAR": "cred-uuid"},
			"tools":           []interface{}{},
		},
	})
	if err == nil {
		t.Fatal("a missing required env var was accepted")
	}
	if !strings.Contains(err.Error(), "GITHUB_PERSONAL_ACCESS_TOKEN") {
		t.Errorf("error does not name the missing variable: %v", err)
	}
}

// A server with no vault reference carries no empty map: absence stays absence,
// so nothing downstream has to tell "{}" apart from "not configured".
func TestNoCredentialRefsKeyWhenNoneGiven(t *testing.T) {
	processor := processorWithCatalog(t, `{}`)

	processed, err := processor.processMCPServers(context.Background(), []interface{}{
		map[string]interface{}{
			"id":           serverID,
			"environments": map[string]interface{}{},
			"tools":        []interface{}{},
		},
	})
	if err != nil {
		t.Fatalf("processing failed: %v", err)
	}
	if _, present := processed[0]["credential_refs"]; present {
		t.Error("an empty credential_refs was invented where the request had none")
	}
}
