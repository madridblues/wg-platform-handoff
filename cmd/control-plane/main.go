package main

import (
	"log"
	"net/http"

	"wg-platform-handoff/internal/api"
	"wg-platform-handoff/internal/config"
)

func main() {
	cfg := config.Load()

	router, err := api.NewRouter(cfg)
	if err != nil {
		log.Fatalf("router init failed: %v", err)
	}

	server := &http.Server{
		Addr:         cfg.HTTPAddr,
		Handler:      router,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	}

	log.Printf("control-plane listening on %s", cfg.HTTPAddr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}
