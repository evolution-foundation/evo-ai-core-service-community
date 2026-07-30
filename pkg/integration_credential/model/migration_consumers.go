package model

// Consumer keys of the migration-state contract: the screens key off these
// exact names to decide whether an inline secret field is still editable.
// // In `model` because both the service and the repository need them, and the
// repository cannot import the service without a cycle.
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
