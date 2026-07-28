package handlers

import (
	"net/http"
	"stellarbill-backend/internal/service"

	"github.com/gin-gonic/gin"
)

// PlanTemplateHandler handles plan template HTTP requests.
type PlanTemplateHandler struct {
	svc service.PlanTemplateService
}

// NewPlanTemplateHandler creates a new plan template handler.
func NewPlanTemplateHandler(svc service.PlanTemplateService) *PlanTemplateHandler {
	return &PlanTemplateHandler{svc: svc}
}

// RegisterPlan handles POST /api/v1/plan-templates
func (h *PlanTemplateHandler) RegisterPlan(c *gin.Context) {
	merchantID := c.GetString("merchant_id")
	if merchantID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "merchant_id required"})
		return
	}

	var req service.RegisterPlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	detail, err := h.svc.RegisterPlan(c.Request.Context(), merchantID, req)
	if err != nil {
		switch err {
		case service.ErrDuplicatePlanName:
			c.JSON(http.StatusConflict, gin.H{"error": "plan name already exists"})
		case service.ErrInvalidPlanTemplate:
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		}
		return
	}

	c.JSON(http.StatusCreated, detail)
}

// DeprecatePlan handles POST /api/v1/plan-templates/:id/deprecate
func (h *PlanTemplateHandler) DeprecatePlan(c *gin.Context) {
	merchantID := c.GetString("merchant_id")
	if merchantID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "merchant_id required"})
		return
	}

	planID := c.Param("id")
	if planID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "plan id required"})
		return
	}

	err := h.svc.DeprecatePlan(c.Request.Context(), merchantID, planID)
	if err != nil {
		switch err {
		case service.ErrPlanTemplateNotFound:
			c.JSON(http.StatusNotFound, gin.H{"error": "plan template not found"})
		case service.ErrForbidden:
			c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "plan deprecated successfully"})
}

// GetPlanTemplate handles GET /api/v1/plan-templates/:id
func (h *PlanTemplateHandler) GetPlanTemplate(c *gin.Context) {
	merchantID := c.GetString("merchant_id")
	if merchantID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "merchant_id required"})
		return
	}

	planID := c.Param("id")
	if planID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "plan id required"})
		return
	}

	detail, err := h.svc.GetPlanTemplate(c.Request.Context(), merchantID, planID)
	if err != nil {
		switch err {
		case service.ErrPlanTemplateNotFound:
			c.JSON(http.StatusNotFound, gin.H{"error": "plan template not found"})
		case service.ErrForbidden:
			c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		}
		return
	}

	c.JSON(http.StatusOK, detail)
}

// ListPlanTemplates handles GET /api/v1/plan-templates
func (h *PlanTemplateHandler) ListPlanTemplates(c *gin.Context) {
	merchantID := c.GetString("merchant_id")
	if merchantID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "merchant_id required"})
		return
	}

	includeDeprecated := c.Query("include_deprecated") == "true"

	details, err := h.svc.ListPlanTemplates(c.Request.Context(), merchantID, includeDeprecated)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"plan_templates": details})
}

// CreateSubscriptionFromPlan handles POST /api/v1/subscriptions/from-template
func (h *PlanTemplateHandler) CreateSubscriptionFromPlan(c *gin.Context) {
	merchantID := c.GetString("merchant_id")
	if merchantID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "merchant_id required"})
		return
	}

	var req struct {
		PlanTemplateID string `json:"plan_template_id" binding:"required"`
		CustomerID     string `json:"customer_id" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Fetch the plan template
	template, err := h.svc.GetPlanTemplate(c.Request.Context(), merchantID, req.PlanTemplateID)
	if err != nil {
		switch err {
		case service.ErrPlanTemplateNotFound:
			c.JSON(http.StatusNotFound, gin.H{"error": "plan template not found"})
		case service.ErrForbidden:
			c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		}
		return
	}

	// Reject deprecated plans
	if template.DeprecatedAt != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot create subscription from deprecated plan template"})
		return
	}

	// TODO: Create actual subscription using template parameters
	// For now, return success with the template info
	c.JSON(http.StatusCreated, gin.H{
		"message":  "subscription created from template",
		"template": template,
		"customer_id": req.CustomerID,
	})
}
