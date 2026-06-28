package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"stellarbill-backend/internal/audit"
	"stellarbill-backend/internal/service"
)

// ExportJobManager defines the interface for creating and querying export jobs.
// The handler depends on this interface rather than a concrete type, making it
// straightforward to test and swap implementations.
type ExportJobManager interface {
	CreateJob(ctx context.Context, tenantID, callerID string, callerRoles []string) (*service.ExportJob, error)
	GetJob(id string) (*service.ExportJob, error)
}

type createExportResponse struct {
	JobID     string `json:"job_id"`
	StatusURL string `json:"status_url"`
	Message   string `json:"message"`
}

type exportStatusResponse struct {
	JobID  string                   `json:"job_id"`
	Status service.ExportJobStatus  `json:"status"`
	Result *service.TenantExportResult `json:"result,omitempty"`
	Error  string                   `json:"error,omitempty"`
}

// NewTenantExportHandler returns a gin.HandlerFunc for POST /api/v1/tenants/me/export.
//
// It enqueues an asynchronous export job that produces a downloadable ZIP
// containing the tenant's plans, subscriptions, and statements. The caller
// must have either the "admin" role or the "merchant" role and match the
// target tenant. Only one export per tenant may be pending or running at a
// time; a second attempt returns 409 Conflict.
//
// The response body contains the job_id and a status_url to poll for
// completion. An audit event with action "tenant_export" is emitted on every
// request — granted, denied, or queued.
//
// Rate-limiting is enforced per-tenant by TenantRateLimitMiddleware applied
// at the route level.
func NewTenantExportHandler(jobManager ExportJobManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		if jobManager == nil {
			RespondWithInternalError(c, "export service unavailable")
			return
		}

		callerID, roles, ok := getAuthContext(c)
		if !ok {
			RespondWithAuthError(c, "unauthorized")
			return
		}

		tenantID, ok := getRequiredStringContextValue(c, "tenantID", "Missing tenant context")
		if !ok {
			return
		}

		isAuthorized := false
		for _, role := range roles {
			if role == "admin" {
				isAuthorized = true
				break
			}
			if role == "merchant" && callerID == tenantID {
				isAuthorized = true
				break
			}
		}
		if !isAuthorized {
			audit.LogAction(c, "tenant_export", "tenant:"+tenantID, "denied", nil)
			RespondWithError(c, http.StatusForbidden, ErrorCodeForbidden, "You do not have permission to export this tenant's data")
			return
		}

		job, err := jobManager.CreateJob(c.Request.Context(), tenantID, callerID, roles)
		if err != nil {
			if errors.Is(err, service.ErrExportInProgress) {
				RespondWithError(c, http.StatusConflict, ErrorCodeConflict, "An export is already in progress for this tenant")
				return
			}
			RespondWithInternalError(c, "Failed to create export job")
			return
		}

		audit.LogAction(c, "tenant_export", "tenant:"+tenantID, "queued", map[string]string{
			"job_id": job.ID,
		})

		c.JSON(http.StatusAccepted, createExportResponse{
			JobID:     job.ID,
			StatusURL: "/api/v1/tenants/me/export/" + job.ID,
			Message:   "Export job created. Poll the status URL for completion.",
		})
	}
}

// NewTenantExportStatusHandler returns a gin.HandlerFunc for
// GET /api/v1/tenants/me/export/:job_id.
//
// It returns the current status of an export job. The caller may only poll
// jobs they created or that belong to their tenant — cross-tenant reads
// return 404 Not Found. The response includes the job status, an optional
// error message, and, on completion, a presigned S3 URL (valid for 24 hours)
// together with the SHA-256 hash of the bundle for tamper detection.
func NewTenantExportStatusHandler(jobManager ExportJobManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		if jobManager == nil {
			RespondWithInternalError(c, "export service unavailable")
			return
		}

		callerID, _, ok := getAuthContext(c)
		if !ok {
			RespondWithAuthError(c, "unauthorized")
			return
		}

		tenantID, ok := getRequiredStringContextValue(c, "tenantID", "Missing tenant context")
		if !ok {
			return
		}

		jobID := c.Param("job_id")
		if jobID == "" {
			RespondWithError(c, http.StatusBadRequest, ErrorCodeBadRequest, "job_id is required")
			return
		}

		job, err := jobManager.GetJob(jobID)
		if err != nil {
			code, errCode, msg := MapServiceErrorToResponse(err)
			RespondWithError(c, code, errCode, msg)
			return
		}

		if job.TenantID != tenantID && callerID != job.CallerID {
			RespondWithError(c, http.StatusNotFound, ErrorCodeNotFound, "Export job not found")
			return
		}

		c.JSON(http.StatusOK, exportStatusResponse{
			JobID:  job.ID,
			Status: job.Status,
			Result: job.Result,
			Error:  job.Error,
		})
	}
}
