package middleware

import (
    "github.com/gin-gonic/gin"
    "stellarbill-backend/internal/correlation"
)

// CorrelationIDMiddleware injects a correlation (job) ID into the request context and Gin context.
func CorrelationIDMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        // Retrieve existing job ID from context if present.
        ctx := c.Request.Context()
        jobID := correlation.JobIDFromContext(ctx)
        if jobID == "" {
            // Generate a new job ID and store it in the context.
            jobID = correlation.NewID()
            ctx = correlation.WithJobID(ctx, jobID)
        }
        // Update the request with the new context containing the job ID.
        c.Request = c.Request.WithContext(ctx)
        // Also store it in Gin context for easy access in handlers/logger.
        c.Set("correlation_id", jobID)
        c.Next()
    }
}
