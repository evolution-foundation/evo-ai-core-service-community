//go:build enterprise

// Self-review probe (EVO-1790 R2): proves GORM picks up the embedded
// TenantField.TenantID on each of the 8 real evo_core_* models via
// Schema.LookUpField. Lives in a sibling package to avoid the import
// cycle (models → tenantfield) while still exercising the real wiring.
//
// If this test fails, the tenantstamp plugin silently no-ops on the
// real models and INSERTs miss the tenant_id stamp — RLS rejects.
package tenantfield_review_test

import (
	"sync"
	"testing"

	agentModel "evo-ai-core-service/pkg/agent/model"
	agentIntegrationModel "evo-ai-core-service/pkg/agent_integration/model"
	apiKeyModel "evo-ai-core-service/pkg/api_key/model"
	customMcpServerModel "evo-ai-core-service/pkg/custom_mcp_server/model"
	customToolModel "evo-ai-core-service/pkg/custom_tool/model"
	folderModel "evo-ai-core-service/pkg/folder/model"
	folderShareModel "evo-ai-core-service/pkg/folder_share/model"
	mcpServerModel "evo-ai-core-service/pkg/mcp_server/model"

	"gorm.io/gorm/schema"
)

func TestEmbed_GORMPicksUpTenantIDOnAll8Models(t *testing.T) {
	cases := []struct {
		name  string
		model interface{}
	}{
		{"Agent", agentModel.Agent{}},
		{"AgentIntegration", agentIntegrationModel.AgentIntegration{}},
		{"ApiKey", apiKeyModel.ApiKey{}},
		{"CustomMcpServer", customMcpServerModel.CustomMcpServer{}},
		{"CustomTool", customToolModel.CustomTool{}},
		{"Folder", folderModel.Folder{}},
		{"FolderShare", folderShareModel.FolderShare{}},
		{"McpServer", mcpServerModel.McpServer{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := schema.Parse(tc.model, &sync.Map{}, schema.NamingStrategy{})
			if err != nil {
				t.Fatalf("Schema.Parse failed: %v", err)
			}
			f := s.LookUpField("tenant_id")
			if f == nil {
				t.Fatalf("Schema.LookUpField(\"tenant_id\") returned nil — embedded TenantField is invisible to GORM; tenantstamp plugin would no-op on this model")
			}
			if f.DBName != "tenant_id" {
				t.Fatalf("expected DBName tenant_id, got %q", f.DBName)
			}
		})
	}
}
