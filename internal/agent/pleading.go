package agent

import (
	"context"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"law-assistant/internal/prompt"
)

// PleadingAgent handles legal document drafting
type PleadingAgent struct {
	chatModel model.ChatModel
}

// NewPleadingAgent creates a new PleadingAgent
func NewPleadingAgent(chatModel model.ChatModel) *PleadingAgent {
	return &PleadingAgent{
		chatModel: chatModel,
	}
}

// Handle processes pleading drafting requests with streaming response
func (a *PleadingAgent) Handle(ctx context.Context, messages []*schema.Message) (*schema.StreamReader[*schema.Message], error) {
	fullMessages := make([]*schema.Message, 0, len(messages)+1)
	fullMessages = append(fullMessages, schema.SystemMessage(prompt.PleadingSystemPrompt))
	fullMessages = append(fullMessages, messages...)

	streamReader, err := a.chatModel.Stream(ctx, fullMessages)
	if err != nil {
		return nil, err
	}

	return streamReader, nil
}
