package model

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino-ext/components/model/openai"

	"law-assistant/internal/config"
)

// NewQwenChatModel creates a new ChatModel instance connected to Qwen via OpenAI-compatible API
func NewQwenChatModel(ctx context.Context, cfg *config.Config) (*openai.ChatModel, error) {
	chatModel, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
		BaseURL: cfg.QwenBaseURL,
		APIKey:  cfg.QwenAPIKey,
		Model:   cfg.QwenModel,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Qwen chat model: %w", err)
	}

	return chatModel, nil
}
