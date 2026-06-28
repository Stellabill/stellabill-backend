package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/service"
)

// SwapHandler handles token swap HTTP requests.
type SwapHandler struct {
	router service.SwapRouter
}

// NewSwapHandler creates a SwapHandler.
func NewSwapHandler(router service.SwapRouter) *SwapHandler {
	return &SwapHandler{router: router}
}

type swapExactInRequest struct {
	TokenIn      string  `json:"token_in" binding:"required"`
	TokenOut     string  `json:"token_out" binding:"required"`
	AmountIn     float64 `json:"amount_in" binding:"required,gt=0"`
	MinAmountOut float64 `json:"min_amount_out" binding:"gte=0"`
}

type swapExactOutRequest struct {
	TokenIn      string  `json:"token_in" binding:"required"`
	TokenOut     string  `json:"token_out" binding:"required"`
	AmountOut    float64 `json:"amount_out" binding:"required,gt=0"`
	MaxAmountIn  float64 `json:"max_amount_in" binding:"required,gt=0"`
}

// SwapExactTokensForTokens godoc
// POST /api/v1/swap/exact-in
func (h *SwapHandler) SwapExactTokensForTokens(c *gin.Context) {
	var req swapExactInRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondWithErrorDetails(c, http.StatusBadRequest, ErrorCodeValidationFailed, err.Error(), nil)
		return
	}

	result, err := h.router.SwapExactTokensForTokens(req.TokenIn, req.TokenOut, req.AmountIn, req.MinAmountOut)
	if err != nil {
		if errors.Is(err, service.ErrInsufficientLiquidity) {
			RespondWithError(c, http.StatusUnprocessableEntity, ErrorCodeBadRequest, err.Error())
			return
		}
		RespondWithInternalError(c, "swap failed")
		return
	}

	c.JSON(http.StatusOK, result)
}

// SwapTokensForExactTokens godoc
// POST /api/v1/swap/exact-out
func (h *SwapHandler) SwapTokensForExactTokens(c *gin.Context) {
	var req swapExactOutRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondWithErrorDetails(c, http.StatusBadRequest, ErrorCodeValidationFailed, err.Error(), nil)
		return
	}

	result, err := h.router.SwapTokensForExactTokens(req.TokenIn, req.TokenOut, req.AmountOut, req.MaxAmountIn)
	if err != nil {
		if errors.Is(err, service.ErrInsufficientLiquidity) {
			RespondWithError(c, http.StatusUnprocessableEntity, ErrorCodeBadRequest, err.Error())
			return
		}
		RespondWithInternalError(c, "swap failed")
		return
	}

	c.JSON(http.StatusOK, result)
}
