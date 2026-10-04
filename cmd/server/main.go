package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"law-assistant/internal/agent"
	"law-assistant/internal/auth"
	"law-assistant/internal/config"
	"law-assistant/internal/handler"
	"law-assistant/internal/model"
	"law-assistant/internal/store"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	log.Printf("Starting AI Law Assistant Server...")
	log.Printf("Using model: %s", cfg.QwenModel)
	log.Printf("API BaseURL: %s", cfg.QwenBaseURL)

	// Initialize Qwen ChatModel
	ctx := context.Background()
	chatModel, err := model.NewQwenChatModel(ctx, cfg)
	if err != nil {
		log.Fatalf("Failed to initialize Qwen model: %v", err)
	}
	log.Printf("Qwen ChatModel initialized successfully")

	// Initialize authentication (fails closed when unconfigured)
	authn, err := auth.New(ctx, auth.Config{
		SupabaseURL: cfg.SupabaseURL,
		JWTSecret:   cfg.SupabaseJWTSecret,
		Disabled:    cfg.AuthDisabled,
	})
	if err != nil {
		log.Fatalf("Failed to initialize authentication: %v", err)
	}

	// Initialize stores
	sessionStore := store.NewSessionStore()
	fileStore := store.NewFileStore(cfg.UploadDir)

	// Initialize agent manager
	agentMgr := agent.NewAgentManager(chatModel)
	log.Printf("Agent manager initialized with %d modules", 6)

	// Initialize HTTP server
	srv := handler.NewServer(cfg, agentMgr, sessionStore, fileStore, authn)
	routes := srv.SetupRoutes()

	httpServer := &http.Server{
		Addr:         fmt.Sprintf(":%s", cfg.ServerPort),
		Handler:      routes,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 120 * time.Second, // Long timeout for streaming
		IdleTimeout:  120 * time.Second,
	}

	// Graceful shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan

		log.Println("Shutting down server...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Printf("Server shutdown error: %v", err)
		}
	}()

	log.Printf("Server listening on http://localhost:%s", cfg.ServerPort)
	log.Printf("Frontend URL: %s", cfg.FrontendURL)

	if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}

	log.Println("Server stopped")
}
