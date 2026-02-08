package agent

import (
	"context"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"law-assistant/internal/prompt"
)

// EvidenceOrgAgent handles evidence organization
type EvidenceOrgAgent struct {
	chatModel model.ChatModel
}

// NewEvidenceOrgAgent creates a new EvidenceOrgAgent
func NewEvidenceOrgAgent(chatModel model.ChatModel) *EvidenceOrgAgent {
	return &EvidenceOrgAgent{
		chatModel: chatModel,
	}
}

// Handle processes evidence organization requests with streaming response
func (a *EvidenceOrgAgent) Handle(ctx context.Context, messages []*schema.Message) (*schema.StreamReader[*schema.Message], error) {
	fullMessages := make([]*schema.Message, 0, len(messages)+1)
	fullMessages = append(fullMessages, schema.SystemMessage(prompt.EvidenceOrgSystemPrompt))
	fullMessages = append(fullMessages, messages...)

	streamReader, err := a.chatModel.Stream(ctx, fullMessages)
	if err != nil {
		return nil, err
	}

	return streamReader, nil
}
