package config

import (
	"fmt"
	"os"
)

// Config holds the application configuration
type Config struct {
	// Server configuration
	ServerPort string

	// Qwen model configuration
	QwenAPIKey  string
	QwenBaseURL string
	QwenModel   string

	// File upload configuration
	UploadDir     string
	MaxUploadSize int64 // in bytes

	// Frontend
	FrontendURL string

	// Authentication (Supabase)
	SupabaseURL       string // https://<project>.supabase.co
	SupabaseJWTSecret string // legacy HS256 secret; optional when the project uses JWKS
	AuthDisabled      bool   // local development only
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

	cfg := &Config{
		ServerPort:    getEnvOrDefault("SERVER_PORT", "8080"),
		QwenAPIKey:    apiKey,
		QwenBaseURL:   getEnvOrDefault("QWEN_BASE_URL", "https://dashscope.aliyuncs.com/compatible-mode/v1"),
		QwenModel:     getEnvOrDefault("QWEN_MODEL", "qwen-max"),
		UploadDir:     getEnvOrDefault("UPLOAD_DIR", "./uploads"),
		MaxUploadSize: 20 * 1024 * 1024, // 20MB
		FrontendURL:   getEnvOrDefault("FRONTEND_URL", "http://localhost:5173"),

		SupabaseURL:       os.Getenv("SUPABASE_URL"),
		SupabaseJWTSecret: os.Getenv("SUPABASE_JWT_SECRET"),
		AuthDisabled:      os.Getenv("AUTH_DISABLED") == "true",
	}

	// Ensure upload directory exists
	if err := os.MkdirAll(cfg.UploadDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create upload directory: %w", err)
	}

	return cfg, nil
}

func getEnvOrDefault(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
