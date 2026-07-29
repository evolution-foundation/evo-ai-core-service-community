package model

// Consumer keys of the migration-state contract. The screens key off these
// exact names to decide whether an inline secret field is still editable
// (EVO-2250 story 2.7).
//
// They live in `model` because both the service (which decides) and the
// repository (which counts) need them, and the repository cannot import the
// service without an import cycle.
const (
	ConsumerCustomTools      = "custom_tools"
	ConsumerCustomMcpServers = "custom_mcp_servers"
	ConsumerKnowledgeNexus   = "knowledge_nexus"
	ConsumerAgentBots        = "agent_bots"
	ConsumerExternalAgents   = "external_agents"
)

// Consumers is the full set the endpoint reports on.
var Consumers = []string{
	ConsumerCustomTools,
	ConsumerCustomMcpServers,
	ConsumerKnowledgeNexus,
	ConsumerAgentBots,
	ConsumerExternalAgents,
}
