package model

import (
	"encoding/json"
	"strings"
	"testing"
)

// Same defect as the custom tool: the header map reached anyone with :read.
func TestToResponseRedactsHeaderValues(t *testing.T) {
	server := CustomMcpServer{
		Name:    "MCP remoto",
		URL:     "https://mcp.example.com",
		Headers: `{"Authorization":"Bearer sk-secreto","Content-Type":"application/json"}`,
	}

	payload, err := json.Marshal(server.ToResponse())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := string(payload)

	if strings.Contains(body, "sk-secreto") {
		t.Errorf("response leaks the header value: %s", body)
	}
	if !strings.Contains(body, "Authorization") {
		t.Errorf("response dropped the header name: %s", body)
	}
	// The URL is not a secret and must survive: it is where the MCP lives.
	if !strings.Contains(body, "https://mcp.example.com") {
		t.Errorf("response dropped the url: %s", body)
	}
}

func TestToResponseExposesCredentialRefs(t *testing.T) {
	server := CustomMcpServer{
		Name:           "MCP remoto",
		CredentialRefs: `{"Authorization":"22222222-2222-2222-2222-222222222222"}`,
	}

	if got := server.ToResponse().CredentialRefs["Authorization"]; got != "22222222-2222-2222-2222-222222222222" {
		t.Errorf("credential_refs = %q, want the reference", got)
	}
}
