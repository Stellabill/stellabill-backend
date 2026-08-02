package grpc

import (
	"context"
	"testing"

	"stellarbill-backend/internal/handlers"

	pb "stellarbill-backend/gen/stellabill/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionServiceServer_ListSubscriptions(t *testing.T) {
	t.Parallel()

	t.Run("empty subscriptions", func(t *testing.T) {
		t.Parallel()
		srv := NewSubscriptionServiceServer(&stubSubscriptionService{})
		resp, err := srv.ListSubscriptions(context.Background(), &pb.ListSubscriptionsRequest{Limit: 10})
		require.NoError(t, err)
		assert.Empty(t, resp.Subscriptions)
		assert.False(t, resp.HasMore)
		assert.Empty(t, resp.NextCursor)
	})

	t.Run("returns all subscriptions", func(t *testing.T) {
		t.Parallel()
		srv := NewSubscriptionServiceServer(&stubSubscriptionService{
			subs: []handlers.Subscription{
				{ID: "sub-1", PlanID: "plan-1", Customer: "cust-1", Status: "active", Amount: "1000", Interval: "month"},
				{ID: "sub-2", PlanID: "plan-2", Customer: "cust-2", Status: "active", Amount: "2000", Interval: "month"},
			},
		})
		resp, err := srv.ListSubscriptions(context.Background(), &pb.ListSubscriptionsRequest{Limit: 10})
		require.NoError(t, err)
		require.Len(t, resp.Subscriptions, 2)
		assert.Equal(t, "sub-1", resp.Subscriptions[0].Id)
		assert.Equal(t, "plan-1", resp.Subscriptions[0].PlanId)
		assert.Equal(t, "cust-1", resp.Subscriptions[0].Customer)
		assert.Equal(t, "active", resp.Subscriptions[0].Status)
		assert.Equal(t, "1000", resp.Subscriptions[0].Amount)
		assert.Equal(t, "month", resp.Subscriptions[0].Interval)
		assert.False(t, resp.HasMore)
	})

	t.Run("pagination", func(t *testing.T) {
		t.Parallel()
		subs := make([]handlers.Subscription, 5)
		for i := 0; i < 5; i++ {
			subs[i] = handlers.Subscription{
				ID:       "sub-" + string(rune('0'+(i+1))),
				Customer: "cust-" + string(rune('0'+(i+1))),
				Status:   "active",
			}
		}
		srv := NewSubscriptionServiceServer(&stubSubscriptionService{subs: subs})

		// First page with limit 2
		resp, err := srv.ListSubscriptions(context.Background(), &pb.ListSubscriptionsRequest{Limit: 2})
		require.NoError(t, err)
		require.Len(t, resp.Subscriptions, 2)
		assert.True(t, resp.HasMore)
		assert.Equal(t, "sub-2", resp.NextCursor)

		// Second page
		resp, err = srv.ListSubscriptions(context.Background(), &pb.ListSubscriptionsRequest{Limit: 2, Cursor: "sub-2"})
		require.NoError(t, err)
		require.Len(t, resp.Subscriptions, 2)
		assert.True(t, resp.HasMore)

		// Third page
		resp, err = srv.ListSubscriptions(context.Background(), &pb.ListSubscriptionsRequest{Limit: 2, Cursor: "sub-4"})
		require.NoError(t, err)
		require.Len(t, resp.Subscriptions, 1)
		assert.False(t, resp.HasMore)
	})

	t.Run("next_billing included", func(t *testing.T) {
		t.Parallel()
		srv := NewSubscriptionServiceServer(&stubSubscriptionService{
			subs: []handlers.Subscription{
				{ID: "sub-1", Customer: "cust-1", NextBilling: "2025-01-01"},
			},
		})
		resp, err := srv.ListSubscriptions(context.Background(), &pb.ListSubscriptionsRequest{Limit: 10})
		require.NoError(t, err)
		require.Len(t, resp.Subscriptions, 1)
		assert.Equal(t, "2025-01-01", resp.Subscriptions[0].NextBilling)
	})

	t.Run("default limit when limit is zero", func(t *testing.T) {
		t.Parallel()
		subs := make([]handlers.Subscription, 15)
		for i := 0; i < 15; i++ {
			subs[i] = handlers.Subscription{ID: "sub-" + string(rune('0'+i))}
		}
		srv := NewSubscriptionServiceServer(&stubSubscriptionService{subs: subs})
		resp, err := srv.ListSubscriptions(context.Background(), &pb.ListSubscriptionsRequest{Limit: 0})
		require.NoError(t, err)
		assert.Len(t, resp.Subscriptions, 10)
		assert.True(t, resp.HasMore)
	})
}

func TestSubscriptionServiceServer_GetSubscription(t *testing.T) {
	t.Parallel()

	t.Run("existing subscription", func(t *testing.T) {
		t.Parallel()
		srv := NewSubscriptionServiceServer(&stubSubscriptionService{
			subs: []handlers.Subscription{
				{ID: "sub-1", PlanID: "plan-1", Customer: "cust-1", Status: "active", Amount: "1000", Interval: "month"},
			},
		})
		resp, err := srv.GetSubscription(context.Background(), &pb.GetSubscriptionRequest{Id: "sub-1"})
		require.NoError(t, err)
		require.NotNil(t, resp.Subscription)
		assert.Equal(t, "sub-1", resp.Subscription.Id)
		assert.Equal(t, "plan-1", resp.Subscription.PlanId)
		assert.Equal(t, "active", resp.Subscription.Status)
	})

	t.Run("non-existent subscription", func(t *testing.T) {
		t.Parallel()
		srv := NewSubscriptionServiceServer(&stubSubscriptionService{
			subs: []handlers.Subscription{{ID: "sub-1"}},
		})
		_, err := srv.GetSubscription(context.Background(), &pb.GetSubscriptionRequest{Id: "nonexistent"})
		require.Error(t, err)
	})

	t.Run("empty id", func(t *testing.T) {
		t.Parallel()
		srv := NewSubscriptionServiceServer(&stubSubscriptionService{})
		_, err := srv.GetSubscription(context.Background(), &pb.GetSubscriptionRequest{Id: ""})
		require.Error(t, err)
	})
}
