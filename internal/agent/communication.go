package agent

import (
	"context"

	"github.com/cloudwego/eino/schema"

	"law-assistant/internal/prompt"
)

// CommunicationAgent handles client communication guidance
type CommunicationAgent struct {
	models ModelProvider
}

// NewCommunicationAgent creates a new CommunicationAgent
func NewCommunicationAgent(models ModelProvider) *CommunicationAgent {
	return &CommunicationAgent{
		models: models,
	}
}

// Handle processes communication guidance requests with streaming response
func (a *CommunicationAgent) Handle(ctx context.Context, modelID string, messages []*schema.Message) (*schema.StreamReader[*schema.Message], error) {
	fullMessages := make([]*schema.Message, 0, len(messages)+1)
	fullMessages = append(fullMessages, schema.SystemMessage(prompt.CommunicationSystemPrompt))
	fullMessages = append(fullMessages, messages...)

	return stream(ctx, a.models, modelID, fullMessages)
}
