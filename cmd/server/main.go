package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"wa-chatbot/internal/config"
	"wa-chatbot/internal/handler"
	"wa-chatbot/internal/service"
	"wa-chatbot/internal/storage"
)

func main() {
	logger := log.New(os.Stdout, "wa-chatbot ", log.LstdFlags|log.LUTC)
	cfg, err := config.Load()
	if err != nil {
		logger.Fatalf("load config: %v", err)
	}

	if cfg.AppEnv == "production" {
		gin.SetMode(gin.ReleaseMode)
	}
	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())
	router.GET("/health", handler.Health)

	openAI := service.NewOpenAIService(cfg)
	gowa := service.NewGOWAService(cfg, logger)
	meTube := service.NewMeTubeService(cfg, logger)

	var memory *storage.PostgresConversationStore
	if cfg.ChatMemoryEnabled {
		memoryCtx, cancelMemory := context.WithTimeout(context.Background(), 15*time.Second)
		memory, err = storage.NewPostgresConversationStore(memoryCtx, cfg.PostgresDSN, cfg.ChatMemoryMaxMessages, cfg.ChatMemoryTTL, cfg.PostgresMaxConns, cfg.PostgresMinConns)
		cancelMemory()
		if err != nil {
			logger.Fatalf("initialize PostgreSQL conversation memory: %v", err)
		}
		defer memory.Close()
		logger.Printf("postgres conversation memory enabled max_messages=%d ttl=%s", cfg.ChatMemoryMaxMessages, cfg.ChatMemoryTTL)
	} else {
		logger.Printf("conversation memory disabled")
	}

	webhook := handler.NewWebhookHandler(cfg, openAI, gowa, meTube, memory, logger)
	router.POST(cfg.WebhookPath, webhook.Handle)
	if cfg.MeTubeEnabled {
		logger.Printf("metube integration enabled base_url=%s hosts=%d", cfg.MeTubeBaseURL, len(cfg.MeTubeAllowedHosts))
	} else {
		logger.Printf("metube integration disabled")
	}

	server := &http.Server{
		Addr:              cfg.AppHost + ":" + cfg.AppPort,
		Handler:           router,
		ReadHeaderTimeout: cfg.ReadTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
	}

	serverErrors := make(chan error, 1)
	go func() {
		logger.Printf("server listening on %s", server.Addr)
		serverErrors <- server.ListenAndServe()
	}()

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)
	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Fatalf("server failed: %v", err)
		}
	case sig := <-shutdown:
		logger.Printf("shutdown signal received: %s", sig)
		ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			logger.Printf("graceful shutdown failed: %v", err)
			_ = server.Close()
		}
	}
}
