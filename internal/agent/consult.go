package agent

import (
	"context"

	"github.com/cloudwego/eino/schema"

	"law-assistant/internal/prompt"
)

// ConsultAgent handles legal consultation conversations
type ConsultAgent struct {
	models ModelProvider
}

// NewConsultAgent creates a new ConsultAgent
func NewConsultAgent(models ModelProvider) *ConsultAgent {
	return &ConsultAgent{
		models: models,
	}
}

// Handle processes legal consultation messages with streaming response
func (a *ConsultAgent) Handle(ctx context.Context, modelID string, messages []*schema.Message) (*schema.StreamReader[*schema.Message], error) {
	// Prepend system prompt
	fullMessages := make([]*schema.Message, 0, len(messages)+1)
	fullMessages = append(fullMessages, schema.SystemMessage(prompt.ConsultSystemPrompt))
	fullMessages = append(fullMessages, messages...)

	// Stream response from LLM
	return stream(ctx, a.models, modelID, fullMessages)
}
