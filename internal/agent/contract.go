package agent

import (
	"context"

	"github.com/cloudwego/eino/schema"

	"law-assistant/internal/prompt"
)

// ContractAgent handles contract review and optimization
type ContractAgent struct {
	models ModelProvider
}

// NewContractAgent creates a new ContractAgent
func NewContractAgent(models ModelProvider) *ContractAgent {
	return &ContractAgent{
		models: models,
	}
}

// Handle processes contract review requests with streaming response
func (a *ContractAgent) Handle(ctx context.Context, modelID string, messages []*schema.Message) (*schema.StreamReader[*schema.Message], error) {
	fullMessages := make([]*schema.Message, 0, len(messages)+1)
	fullMessages = append(fullMessages, schema.SystemMessage(prompt.ContractSystemPrompt))
	fullMessages = append(fullMessages, messages...)

	return stream(ctx, a.models, modelID, fullMessages)
}
