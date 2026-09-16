package model

import (
	"fmt"

	"github.com/google/uuid"
)

const (
	ConsumerKindIntegration = "integration"
	ConsumerKindTool        = "tool"
	ConsumerKindMCP         = "mcp"
	ConsumerKindAgent       = "agent"
	ConsumerKindChannelBot  = "channel_bot"
)

// CredentialConsumer is who holds a credential, for the client to label in its
// own language. For an integration and a channel bot `Name` is the provider.
type CredentialConsumer struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
	Key  string `json:"key,omitempty"`
}

// Label is the pt-BR display string clients read before the structured shape
// existed.
func (c CredentialConsumer) Label() string {
	switch c.Kind {
	case ConsumerKindIntegration:
		return fmt.Sprintf("Integração %s", c.Name)
	case ConsumerKindTool:
		return fmt.Sprintf("Ferramenta %s [%s]", c.Name, c.Key)
	case ConsumerKindMCP:
		return fmt.Sprintf("MCP %s [%s]", c.Name, c.Key)
	case ConsumerKindAgent:
		return fmt.Sprintf("Agente %s [%s]", c.Name, c.Key)
	case ConsumerKindChannelBot:
		return fmt.Sprintf("Bot de canal (%s)", c.Name)
	default:
		return c.Name
	}
}

// CredentialReference is one consumer pointing at one vault credential.
type CredentialReference struct {
	CredentialID uuid.UUID
	Consumer     CredentialConsumer
}
