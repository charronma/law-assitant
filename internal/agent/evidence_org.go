package agent

import (
	"context"

	"github.com/cloudwego/eino/schema"

	"law-assistant/internal/prompt"
)

// EvidenceOrgAgent handles evidence organization
type EvidenceOrgAgent struct {
	models ModelProvider
}

// NewEvidenceOrgAgent creates a new EvidenceOrgAgent
func NewEvidenceOrgAgent(models ModelProvider) *EvidenceOrgAgent {
	return &EvidenceOrgAgent{
		models: models,
	}
}

// Handle processes evidence organization requests with streaming response
func (a *EvidenceOrgAgent) Handle(ctx context.Context, modelID string, messages []*schema.Message) (*schema.StreamReader[*schema.Message], error) {
	fullMessages := make([]*schema.Message, 0, len(messages)+1)
	fullMessages = append(fullMessages, schema.SystemMessage(prompt.EvidenceOrgSystemPrompt))
	fullMessages = append(fullMessages, messages...)

	return stream(ctx, a.models, modelID, fullMessages)
}
