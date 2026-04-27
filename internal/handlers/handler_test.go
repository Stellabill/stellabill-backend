package handlers

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestNewHandler(t *testing.T) {
	mockPlans := new(MockPlanService)
	mockSubs := new(MockSubscriptionService)

	h := NewHandler(mockPlans, mockSubs)

	assert.NotNil(t, h)
	assert.Equal(t, mockPlans, h.Plans)
	assert.Equal(t, mockSubs, h.Subscriptions)
}

