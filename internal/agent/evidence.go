package agent

import (
	"context"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"law-assistant/internal/prompt"
)

// EvidenceAgent handles evidence collection guidance
type EvidenceAgent struct {
	chatModel model.ChatModel
}

// NewEvidenceAgent creates a new EvidenceAgent
func NewEvidenceAgent(chatModel model.ChatModel) *EvidenceAgent {
	return &EvidenceAgent{
		chatModel: chatModel,
	}
}

// Handle processes evidence guidance requests with streaming response
func (a *EvidenceAgent) Handle(ctx context.Context, messages []*schema.Message) (*schema.StreamReader[*schema.Message], error) {
	fullMessages := make([]*schema.Message, 0, len(messages)+1)
	fullMessages = append(fullMessages, schema.SystemMessage(prompt.EvidenceSystemPrompt))
	fullMessages = append(fullMessages, messages...)

	streamReader, err := a.chatModel.Stream(ctx, fullMessages)
	if err != nil {
		return nil, err
	}

	return streamReader, nil
}
