package main

import (
	"log"
	"os"

	"database/sql"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/config"
	"stellarbill-backend/internal/routes"
	"stellabill-backend/internal/logger"
	"stellabill-backend/internal/middleware"
	"stellabill-backend/internal/routes"
)

func main() {
	cfg := config.Load()
	if cfg.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.Default()
	routes.Register(router)

	addr := ":" + cfg.Port
	if p := os.Getenv("PORT"); p != "" {
		addr = ":" + p
	}
	log.Printf("Stellarbill backend listening on %s", addr)
	if err := router.Run(addr); err != nil {
		log.Fatal(err)
	}

	logger.Init()

	r := gin.New()

	r.Use(middleware.RecoveryLogger())
	r.Use(middleware.RequestLogger())

	var db *sql.DB = nil // existing or future DB

	routes.RegisterRoutes(r, db)

	r.Run()
}
