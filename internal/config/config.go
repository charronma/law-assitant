package config

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"law-assistant/internal/model"
)

// Config holds the application configuration
type Config struct {
	// Server configuration
	ServerPort string

	// Qwen model configuration
	QwenAPIKey  string
	QwenBaseURL string
	// QwenModel is the default model (QWEN_MODEL). It must be one of QwenModels.
	QwenModel string
	// QwenModels is the allow-list of models users may pick (QWEN_MODELS),
	// in display order.
	QwenModels []string

	// File upload configuration
	UploadDir     string
	MaxUploadSize int64 // in bytes

	// Abuse / cost controls (0 disables the rate and concurrency limits)
	ChatRatePerMinute  int // chat requests per user per minute
	UploadRatePerMin   int // uploads per user per minute
	MaxConcurrentChats int // simultaneous chat streams per user
	MaxMessageChars    int // longest single user message, in characters
	MaxHistoryChars    int // conversation history sent to the model, in characters

	// Frontend
	FrontendURL string

	// Authentication (Supabase)
	SupabaseURL       string // https://<project>.supabase.co
	SupabaseJWTSecret string // legacy HS256 secret; optional when the project uses JWKS
	// Publishable (or legacy anon) key. With SUPABASE_URL it switches
	// conversations from memory to Supabase Postgres. Public by design.
	SupabasePublishableKey string
	AuthDisabled           bool // local development only
}

// Load reads configuration from environment variables
// API Key 获取方式：https://bailian.console.aliyun.com/ -> API-KEY 管理
// 支持两种环境变量名：DASHSCOPE_API_KEY（百炼平台标准）或 QWEN_API_KEY
func Load() (*Config, error) {
	apiKey := os.Getenv("DASHSCOPE_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("QWEN_API_KEY")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("请设置 DASHSCOPE_API_KEY 环境变量（从 https://bailian.console.aliyun.com/ 获取 API Key）")
	}

	models := ParseModelList(os.Getenv("QWEN_MODELS"))
	if len(models) == 0 {
		models = model.DefaultIDs()
	}
	defaultModel := getEnvOrDefault("QWEN_MODEL", model.DefaultModelID)
	if !contains(models, defaultModel) {
		return nil, fmt.Errorf("默认模型 %q（QWEN_MODEL，未设置时为 %s）不在 QWEN_MODELS 白名单内：%s。请把它加入 QWEN_MODELS，或用 QWEN_MODEL 指定白名单内的模型",
			defaultModel, model.DefaultModelID, strings.Join(models, ","))
	}

	cfg := &Config{
		// SERVER_PORT wins; many platforms (Render, Railway, Fly, Cloud Run) inject PORT.
		ServerPort:    getEnvOrDefault("SERVER_PORT", getEnvOrDefault("PORT", "8080")),
		QwenAPIKey:    apiKey,
		QwenBaseURL:   getEnvOrDefault("QWEN_BASE_URL", "https://dashscope.aliyuncs.com/compatible-mode/v1"),
		QwenModel:     defaultModel,
		QwenModels:    models,
		UploadDir:     getEnvOrDefault("UPLOAD_DIR", "./uploads"),
		MaxUploadSize: 20 * 1024 * 1024, // 20MB
		FrontendURL:   getEnvOrDefault("FRONTEND_URL", "http://localhost:5173"),

		ChatRatePerMinute:  envInt("CHAT_RATE_PER_MINUTE", 20),
		UploadRatePerMin:   envInt("UPLOAD_RATE_PER_MINUTE", 10),
		MaxConcurrentChats: envInt("MAX_CONCURRENT_CHATS", 2),
		MaxMessageChars:    envInt("MAX_MESSAGE_CHARS", 8000),
		MaxHistoryChars:    envInt("MAX_HISTORY_CHARS", 30000),

		SupabaseURL:            os.Getenv("SUPABASE_URL"),
		SupabaseJWTSecret:      os.Getenv("SUPABASE_JWT_SECRET"),
		SupabasePublishableKey: getEnvOrDefault("SUPABASE_PUBLISHABLE_KEY", os.Getenv("SUPABASE_ANON_KEY")),
		AuthDisabled:           os.Getenv("AUTH_DISABLED") == "true",
	}

	// Ensure upload directory exists
	if err := os.MkdirAll(cfg.UploadDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create upload directory: %w", err)
	}

	return cfg, nil
}

// ParseModelList splits a comma-separated list of model ids, trimming spaces
// and dropping blanks and duplicates while keeping the first-seen order.
func ParseModelList(s string) []string {
	var out []string
	seen := make(map[string]bool)
	for _, part := range strings.Split(s, ",") {
		id := strings.TrimSpace(part)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// envInt reads a non-negative integer; unset or invalid values fall back to def.
func envInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		log.Printf("WARNING: ignoring invalid %s=%q, using %d", key, v, def)
		return def
	}
	return n
}

func getEnvOrDefault(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
