package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"wg-platform-handoff/internal/config"
	"wg-platform-handoff/internal/gateway"
)

func main() {
	cfg := config.Load()

	agent := gateway.NewAgent(cfg)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	log.Println("gateway-agent started")
	if err := agent.Run(ctx); err != nil {
		log.Fatalf("gateway-agent failed: %v", err)
	}
}
