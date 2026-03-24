package handlers

import (
	"testing"
	"github.com/stretchr/testify/assert"
)

func TestNewHandler(t *testing.T) {
	mockPlans := new(MockPlanService)
	mockSubs := new(MockSubscriptionService)
	
	h := NewHandler(mockPlans, mockSubs)
	
	assert.NotNil(t, h)
	assert.Equal(t, mockPlans, h.Plans)
	assert.Equal(t, mockSubs, h.Subscriptions)
}
