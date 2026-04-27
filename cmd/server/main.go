package main

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"stellabill-backend/internal/config"
	"stellabill-backend/internal/routes"
	"stellabill-backend/internal/shutdown"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	router := gin.New()
	router.Use(gin.Recovery())

	routes.Register(router)

	addr := fmt.Sprintf(":%d", cfg.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  time.Duration(cfg.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(cfg.WriteTimeout) * time.Second,
		IdleTimeout:  time.Duration(cfg.IdleTimeout) * time.Second,
	}

	gracefulShutdown := shutdown.NewGracefulShutdown(srv, 30*time.Second, 20*time.Second)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	go gracefulShutdown.ListenForShutdownSignals()
	gracefulShutdown.Wait()

	log.Println("Server shutdown completed")
}

