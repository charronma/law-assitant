package agent

import (
	"context"

	"github.com/cloudwego/eino/schema"

	"law-assistant/internal/prompt"
)

// EvidenceAgent handles evidence collection guidance
type EvidenceAgent struct {
	models ModelProvider
}

// NewEvidenceAgent creates a new EvidenceAgent
func NewEvidenceAgent(models ModelProvider) *EvidenceAgent {
	return &EvidenceAgent{
		models: models,
	}
}

// Handle processes evidence guidance requests with streaming response
func (a *EvidenceAgent) Handle(ctx context.Context, modelID string, messages []*schema.Message) (*schema.StreamReader[*schema.Message], error) {
	fullMessages := make([]*schema.Message, 0, len(messages)+1)
	fullMessages = append(fullMessages, schema.SystemMessage(prompt.EvidenceSystemPrompt))
	fullMessages = append(fullMessages, messages...)

	return stream(ctx, a.models, modelID, fullMessages)
}
