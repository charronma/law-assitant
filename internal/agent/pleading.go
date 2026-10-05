package agent

import (
	"context"

	"github.com/cloudwego/eino/schema"

	"law-assistant/internal/prompt"
)

// PleadingAgent handles legal document drafting
type PleadingAgent struct {
	models ModelProvider
}

// NewPleadingAgent creates a new PleadingAgent
func NewPleadingAgent(models ModelProvider) *PleadingAgent {
	return &PleadingAgent{
		models: models,
	}
}

// Handle processes pleading drafting requests with streaming response
func (a *PleadingAgent) Handle(ctx context.Context, modelID string, messages []*schema.Message) (*schema.StreamReader[*schema.Message], error) {
	fullMessages := make([]*schema.Message, 0, len(messages)+1)
	fullMessages = append(fullMessages, schema.SystemMessage(prompt.PleadingSystemPrompt))
	fullMessages = append(fullMessages, messages...)

	return stream(ctx, a.models, modelID, fullMessages)
}
