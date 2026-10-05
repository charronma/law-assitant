package agent

import "fmt"

// AgentManager manages all agent instances and routes requests
type AgentManager struct {
	agents map[ModuleType]Agent
}

// NewAgentManager creates a new agent manager with all agents initialized
func NewAgentManager(models ModelProvider) *AgentManager {
	agents := map[ModuleType]Agent{
		ModuleConsult:       NewConsultAgent(models),
		ModulePleading:      NewPleadingAgent(models),
		ModuleContract:      NewContractAgent(models),
		ModuleEvidenceOrg:   NewEvidenceOrgAgent(models),
		ModuleEvidence:      NewEvidenceAgent(models),
		ModuleCommunication: NewCommunicationAgent(models),
	}

	return &AgentManager{
		agents: agents,
	}
}

// GetAgent returns the agent for the specified module
func (m *AgentManager) GetAgent(module ModuleType) (Agent, error) {
	agent, ok := m.agents[module]
	if !ok {
		return nil, fmt.Errorf("unknown module: %s", module)
	}
	return agent, nil
}

// GetContractAgent returns the contract agent with document handling capability
func (m *AgentManager) GetContractAgent() *ContractAgent {
	return m.agents[ModuleContract].(*ContractAgent)
}
