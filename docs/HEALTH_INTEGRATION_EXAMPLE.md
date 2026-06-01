// health_routes.go - Example integration of health endpoints into your router
// This shows how to register the health check endpoints in your main application

package routes

import (
	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/handlers"
)

// RegisterHealthRoutes registers all health check endpoints
func RegisterHealthRoutes(router *gin.Engine, h *handlers.Handler) {
	// Kubernetes liveness probe - indicates app is running
	// Does NOT check dependencies (simple HTTP response)
	router.GET("/health/live", h.LivenessProbe)

	// Kubernetes readiness probe - indicates app is ready for traffic
	// Checks critical dependencies; returns 503 if unhealthy
	router.GET("/health/ready", h.ReadinessProbe)

	// Detailed health information for monitoring/dashboards
	// Shows all dependency details regardless of status
	router.GET("/health", h.HealthDetails)
	router.GET("/health/detailed", h.HealthDetails)  // Alias for clarity
}

/* 
INTEGRATION EXAMPLE:

In your cmd/server/main.go:

	import (
		"database/sql"
		"github.com/gin-gonic/gin"
		"stellarbill-backend/internal/handlers"
		"stellarbill-backend/internal/routes"
		"stellarbill-backend/internal/outbox"
	)

	func main() {
		// ... existing code ...
		
		// Initialize database and services
		db, _ := sql.Open("postgres", dbURL)
		defer db.Close()

		outboxManager := outbox.NewManager(db)  // Implements OutboxHealther
		
		planSvc := services.NewPlanService(db)
		subSvc := services.NewSubscriptionService(db)
		
		// Initialize handler WITH dependencies for health checks
		handler := handlers.NewHandlerWithDependencies(
			planSvc,
			subSvc,
			db,             // Implements DBPinger interface
			outboxManager,  // Implements OutboxHealther interface
		)

		// Create router and register all routes
		router := gin.New()
		
		// Register health endpoints first (high priority)
		routes.RegisterHealthRoutes(router, handler)
		
		// Register other application endpoints
		routes.Register(router, handler)
		
		// Start server
		srv := &http.Server{
			Addr:    fmt.Sprintf(":%d", cfg.Port),
			Handler: router,
		}
		srv.ListenAndServe()
	}

KUBERNETES DEPLOYMENT EXAMPLE:

	apiVersion: apps/v1
	kind: Deployment
	metadata:
	  name: stellarbill-backend
	spec:
	  template:
	    spec:
	      containers:
	      - name: api
	        image: stellarbill-backend:latest
	        ports:
	        - containerPort: 8080
	        
	        # Liveness: restart if app hangs
	        livenessProbe:
	          httpGet:
	            path: /health/live
	            port: 8080
	          initialDelaySeconds: 10
	          periodSeconds: 10
	          timeoutSeconds: 5
	          failureThreshold: 3
	        
	        # Readiness: stop routing traffic if dependencies down
	        readinessProbe:
	          httpGet:
	            path: /health/ready
	            port: 8080
	          initialDelaySeconds: 5
	          periodSeconds: 5
	          timeoutSeconds: 10
	          failureThreshold: 2
*/
