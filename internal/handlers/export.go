package handlers

import (
	"net/http"
	"os"

	"github.com/gin-gonic/gin"

	"stellarbill-backend/internal/service"
	"stellarbill-backend/internal/storage/s3"
)

// exportRequest is the JSON body for POST /api/admin/statements/export.
type exportRequest struct {
	TenantID   string `json:"tenant_id"`
	CustomerID string `json:"customer_id"`
}

// exportResponse is the JSON body returned on success.
type exportResponse struct {
	ObjectKey string `json:"object_key"`
	URL       string `json:"url"`
	ExpiresAt string `json:"expires_at"`
}

// NewExportStatementsHandler returns a gin.HandlerFunc for
// POST /api/admin/statements/export.
//
// It reads tenant_id and customer_id from the JSON body, enforces cross-tenant
// isolation via StatementService.ExportStatements (only admins or the owning
// merchant may export), uploads a gzipped CSV to S3, and returns a 15-min
// presigned GET URL.
//
// S3 credentials are read from environment variables:
//
//	S3_REGION, S3_BUCKET, S3_ACCESS_KEY_ID, S3_SECRET_ACCESS_KEY, S3_ENDPOINT (optional)
//
// The uploader argument allows tests to inject a mock without touching env vars.
// Pass nil to use the default env-var-configured client.
func NewExportStatementsHandler(svc service.StatementService, uploader s3.S3Uploader) gin.HandlerFunc {
	return func(c *gin.Context) {
		if svc == nil {
			RespondWithInternalError(c, "service unavailable")
			return
		}

		callerID, roles, ok := getAuthContext(c)
		if !ok {
			RespondWithAuthError(c, "unauthorized")
			return
		}

		var req exportRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			RespondWithError(c, http.StatusBadRequest, ErrorCodeBadRequest, "invalid request body")
			return
		}
		if req.TenantID == "" {
			RespondWithError(c, http.StatusBadRequest, ErrorCodeBadRequest, "tenant_id is required")
			return
		}
		if req.CustomerID == "" {
			RespondWithError(c, http.StatusBadRequest, ErrorCodeBadRequest, "customer_id is required")
			return
		}

		u := uploader
		if u == nil {
			u = s3.New(s3.Config{
				Region:          os.Getenv("S3_REGION"),
				Bucket:          os.Getenv("S3_BUCKET"),
				AccessKeyID:     os.Getenv("S3_ACCESS_KEY_ID"),
				SecretAccessKey: os.Getenv("S3_SECRET_ACCESS_KEY"),
				Endpoint:        os.Getenv("S3_ENDPOINT"),
			})
		}

		result, err := svc.ExportStatements(
			c.Request.Context(),
			callerID,
			roles,
			req.TenantID,
			req.CustomerID,
			u,
		)
		if err != nil {
			code, errCode, msg := MapServiceErrorToResponse(err)
			RespondWithError(c, code, errCode, msg)
			return
		}

		c.JSON(http.StatusOK, exportResponse{
			ObjectKey: result.ObjectKey,
			URL:       result.URL,
			ExpiresAt: result.ExpiresAt.Format("2006-01-02T15:04:05Z"),
		})
	}
}
