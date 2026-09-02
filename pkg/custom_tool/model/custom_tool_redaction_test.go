package model

import (
	"encoding/json"
	"strings"
	"testing"
)

// Until story 2.4 the header map was echoed verbatim: anyone holding
// `ai_custom_tools:read` got every stored credential back, and read is a
// strictly lower bar than the create/update that set it.
func TestToResponseRedactsHeaderValues(t *testing.T) {
	tool := CustomTool{
		Name:    "Tool com auth",
		Headers: `{"Authorization":"Bearer sk-secreto","X-Tenant-Auth":"outro-segredo","Content-Type":"application/json"}`,
	}

	payload, err := json.Marshal(tool.ToResponse())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := string(payload)

	for _, secret := range []string{"sk-secreto", "outro-segredo"} {
		if strings.Contains(body, secret) {
			t.Errorf("response leaks %q: %s", secret, body)
		}
	}

	// The header NAMES survive, so the screen still renders what is configured.
	for _, name := range []string{"Authorization", "X-Tenant-Auth"} {
		if !strings.Contains(body, name) {
			t.Errorf("response dropped the header name %q: %s", name, body)
		}
	}
	// A non-secret header keeps its value.
	if !strings.Contains(body, "application/json") {
		t.Errorf("Content-Type lost its value: %s", body)
	}
}

func TestToResponseExposesCredentialRefs(t *testing.T) {
	tool := CustomTool{
		Name:           "Tool",
		Headers:        `{"Authorization":"Bearer inline"}`,
		CredentialRefs: `{"Authorization":"11111111-1111-1111-1111-111111111111"}`,
	}

	response := tool.ToResponse()

	// The reference is not a secret: it is an id, and the screen needs it to
	// show which credential each header points at.
	if response.CredentialRefs["Authorization"] != "11111111-1111-1111-1111-111111111111" {
		t.Errorf("credential_refs = %v, want the reference", response.CredentialRefs)
	}
}
