package service

import (
	"errors"
	"math"
)

// ErrInsufficientLiquidity is returned when a swap cannot be fulfilled.
var ErrInsufficientLiquidity = errors.New("insufficient liquidity")

// SwapResult holds the output of a swap operation.
type SwapResult struct {
	TokenIn      string  `json:"token_in"`
	TokenOut     string  `json:"token_out"`
	AmountIn     float64 `json:"amount_in"`
	AmountOut    float64 `json:"amount_out"`
	PriceImpact  float64 `json:"price_impact"`
	Fee          float64 `json:"fee"`
}

// SwapRouter defines the interface for token swap operations.
type SwapRouter interface {
	// SwapExactTokensForTokens swaps an exact amountIn of tokenIn for as many tokenOut as possible.
	SwapExactTokensForTokens(tokenIn, tokenOut string, amountIn, minAmountOut float64) (*SwapResult, error)
	// SwapTokensForExactTokens swaps as few tokenIn as possible for an exact amountOut of tokenOut.
	SwapTokensForExactTokens(tokenIn, tokenOut string, amountOut, maxAmountIn float64) (*SwapResult, error)
}

// mockSwapRouter is a constant-product AMM simulation (x*y=k).
type mockSwapRouter struct {
	// reserveA and reserveB represent the pool reserves for a single pair.
	reserveA float64
	reserveB float64
	feeRate  float64 // e.g. 0.003 = 0.3%
}

// NewSwapRouter returns a SwapRouter backed by a mock AMM pool.
func NewSwapRouter() SwapRouter {
	return &mockSwapRouter{
		reserveA: 1_000_000,
		reserveB: 1_000_000,
		feeRate:  0.003,
	}
}

// SwapExactTokensForTokens: given exact amountIn, compute amountOut via x*y=k.
func (r *mockSwapRouter) SwapExactTokensForTokens(tokenIn, tokenOut string, amountIn, minAmountOut float64) (*SwapResult, error) {
	if amountIn <= 0 {
		return nil, errors.New("amountIn must be positive")
	}
	fee := math.Round(amountIn*r.feeRate*1e8) / 1e8
	amountInAfterFee := amountIn - fee

	// constant product: amountOut = reserveB * amountInAfterFee / (reserveA + amountInAfterFee)
	amountOut := r.reserveB * amountInAfterFee / (r.reserveA + amountInAfterFee)
	amountOut = math.Round(amountOut*1e8) / 1e8

	if amountOut < minAmountOut {
		return nil, ErrInsufficientLiquidity
	}

	priceImpact := math.Round((amountInAfterFee/(r.reserveA+amountInAfterFee))*10000) / 100

	return &SwapResult{
		TokenIn:     tokenIn,
		TokenOut:    tokenOut,
		AmountIn:    amountIn,
		AmountOut:   amountOut,
		PriceImpact: priceImpact,
		Fee:         fee,
	}, nil
}

// SwapTokensForExactTokens: given exact amountOut, compute required amountIn via x*y=k.
func (r *mockSwapRouter) SwapTokensForExactTokens(tokenIn, tokenOut string, amountOut, maxAmountIn float64) (*SwapResult, error) {
	if amountOut <= 0 {
		return nil, errors.New("amountOut must be positive")
	}
	if amountOut >= r.reserveB {
		return nil, ErrInsufficientLiquidity
	}

	// amountInBeforeFee = reserveA * amountOut / (reserveB - amountOut)
	amountInBeforeFee := r.reserveA * amountOut / (r.reserveB - amountOut)
	fee := math.Round(amountInBeforeFee*r.feeRate*1e8) / 1e8
	amountIn := math.Round((amountInBeforeFee+fee)*1e8) / 1e8

	if amountIn > maxAmountIn {
		return nil, ErrInsufficientLiquidity
	}

	priceImpact := math.Round((amountOut/(r.reserveB))*10000) / 100

	return &SwapResult{
		TokenIn:     tokenIn,
		TokenOut:    tokenOut,
		AmountIn:    amountIn,
		AmountOut:   amountOut,
		PriceImpact: priceImpact,
		Fee:         fee,
	}, nil
}
