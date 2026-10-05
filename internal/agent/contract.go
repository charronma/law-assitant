package agent

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/schema"

	"law-assistant/internal/prompt"
)

// ContractAgent handles contract review and optimization
type ContractAgent struct {
	models ModelProvider
}

// NewContractAgent creates a new ContractAgent
func NewContractAgent(models ModelProvider) *ContractAgent {
	return &ContractAgent{
		models: models,
	}
}

// Handle processes contract review requests with streaming response
func (a *ContractAgent) Handle(ctx context.Context, modelID string, messages []*schema.Message) (*schema.StreamReader[*schema.Message], error) {
	fullMessages := make([]*schema.Message, 0, len(messages)+1)
	fullMessages = append(fullMessages, schema.SystemMessage(prompt.ContractSystemPrompt))
	fullMessages = append(fullMessages, messages...)

	return stream(ctx, a.models, modelID, fullMessages)
}

// HandleWithDocument processes contract review with document content
func (a *ContractAgent) HandleWithDocument(ctx context.Context, modelID string, messages []*schema.Message, documentContent string) (*schema.StreamReader[*schema.Message], error) {
	fullMessages := make([]*schema.Message, 0, len(messages)+2)
	fullMessages = append(fullMessages, schema.SystemMessage(prompt.ContractSystemPrompt))

	// Inject document content as a system message
	if documentContent != "" {
		docMsg := schema.SystemMessage(fmt.Sprintf("以下是用户上传的合同文档内容，请对其进行审查：\n\n---\n%s\n---", documentContent))
		fullMessages = append(fullMessages, docMsg)
	}

	fullMessages = append(fullMessages, messages...)

	return stream(ctx, a.models, modelID, fullMessages)
}
