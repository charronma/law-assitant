package agent

import (
	"context"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"law-assistant/internal/prompt"
)

// CommunicationAgent handles client communication guidance
type CommunicationAgent struct {
	chatModel model.ChatModel
}

// NewCommunicationAgent creates a new CommunicationAgent
func NewCommunicationAgent(chatModel model.ChatModel) *CommunicationAgent {
	return &CommunicationAgent{
		chatModel: chatModel,
	}
}

// Handle processes communication guidance requests with streaming response
func (a *CommunicationAgent) Handle(ctx context.Context, messages []*schema.Message) (*schema.StreamReader[*schema.Message], error) {
	fullMessages := make([]*schema.Message, 0, len(messages)+1)
	fullMessages = append(fullMessages, schema.SystemMessage(prompt.CommunicationSystemPrompt))
	fullMessages = append(fullMessages, messages...)

	streamReader, err := a.chatModel.Stream(ctx, fullMessages)
	if err != nil {
		return nil, err
	}

	return streamReader, nil
}
