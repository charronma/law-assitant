package agent

import (
	"context"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"law-assistant/internal/prompt"
)

// ConsultAgent handles legal consultation conversations
type ConsultAgent struct {
	chatModel model.ChatModel
}

// NewConsultAgent creates a new ConsultAgent
func NewConsultAgent(chatModel model.ChatModel) *ConsultAgent {
	return &ConsultAgent{
		chatModel: chatModel,
	}
}

// Handle processes legal consultation messages with streaming response
func (a *ConsultAgent) Handle(ctx context.Context, messages []*schema.Message) (*schema.StreamReader[*schema.Message], error) {
	// Prepend system prompt
	fullMessages := make([]*schema.Message, 0, len(messages)+1)
	fullMessages = append(fullMessages, schema.SystemMessage(prompt.ConsultSystemPrompt))
	fullMessages = append(fullMessages, messages...)

	// Stream response from LLM
	streamReader, err := a.chatModel.Stream(ctx, fullMessages)
	if err != nil {
		return nil, err
	}

	return streamReader, nil
}
