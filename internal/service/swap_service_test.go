package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSwapExactTokensForTokens_Success(t *testing.T) {
	router := NewSwapRouter()
	result, err := router.SwapExactTokensForTokens("USDC", "XLM", 100.0, 0.0)
	require.NoError(t, err)
	assert.Equal(t, "USDC", result.TokenIn)
	assert.Equal(t, "XLM", result.TokenOut)
	assert.Equal(t, 100.0, result.AmountIn)
	assert.Greater(t, result.AmountOut, 0.0)
	assert.Greater(t, result.Fee, 0.0)
}

func TestSwapExactTokensForTokens_MinAmountOutNotMet(t *testing.T) {
	router := NewSwapRouter()
	_, err := router.SwapExactTokensForTokens("USDC", "XLM", 1.0, 999999.0)
	assert.ErrorIs(t, err, ErrInsufficientLiquidity)
}

func TestSwapExactTokensForTokens_InvalidAmount(t *testing.T) {
	router := NewSwapRouter()
	_, err := router.SwapExactTokensForTokens("USDC", "XLM", 0, 0)
	assert.Error(t, err)
}

func TestSwapTokensForExactTokens_Success(t *testing.T) {
	router := NewSwapRouter()
	result, err := router.SwapTokensForExactTokens("USDC", "XLM", 100.0, 999999.0)
	require.NoError(t, err)
	assert.Equal(t, "USDC", result.TokenIn)
	assert.Equal(t, "XLM", result.TokenOut)
	assert.Equal(t, 100.0, result.AmountOut)
	assert.Greater(t, result.AmountIn, 0.0)
	assert.Greater(t, result.Fee, 0.0)
}

func TestSwapTokensForExactTokens_MaxAmountInExceeded(t *testing.T) {
	router := NewSwapRouter()
	_, err := router.SwapTokensForExactTokens("USDC", "XLM", 100.0, 0.001)
	assert.ErrorIs(t, err, ErrInsufficientLiquidity)
}

func TestSwapTokensForExactTokens_InvalidAmount(t *testing.T) {
	router := NewSwapRouter()
	_, err := router.SwapTokensForExactTokens("USDC", "XLM", 0, 100.0)
	assert.Error(t, err)
}

func TestSwapTokensForExactTokens_ExceedsReserve(t *testing.T) {
	router := NewSwapRouter()
	_, err := router.SwapTokensForExactTokens("USDC", "XLM", 2_000_000.0, 999999999.0)
	assert.ErrorIs(t, err, ErrInsufficientLiquidity)
}
