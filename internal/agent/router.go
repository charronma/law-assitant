package agent

import (
	"fmt"

	"github.com/cloudwego/eino/components/model"
)

// AgentManager manages all agent instances and routes requests
type AgentManager struct {
	agents map[ModuleType]Agent
}

// NewAgentManager creates a new agent manager with all agents initialized
func NewAgentManager(chatModel model.ChatModel) *AgentManager {
	agents := map[ModuleType]Agent{
		ModuleConsult:       NewConsultAgent(chatModel),
		ModulePleading:      NewPleadingAgent(chatModel),
		ModuleContract:      NewContractAgent(chatModel),
		ModuleEvidenceOrg:   NewEvidenceOrgAgent(chatModel),
		ModuleEvidence:      NewEvidenceAgent(chatModel),
		ModuleCommunication: NewCommunicationAgent(chatModel),
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
