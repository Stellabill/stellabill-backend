package grpc

import (
	"context"
	"testing"

	"stellarbill-backend/internal/handlers"

	pb "stellarbill-backend/gen/stellabill/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlanServiceServer_ListPlans(t *testing.T) {
	t.Parallel()

	t.Run("empty plans", func(t *testing.T) {
		t.Parallel()
		srv := NewPlanServiceServer(&stubPlanService{})
		resp, err := srv.ListPlans(context.Background(), &pb.ListPlansRequest{Limit: 10})
		require.NoError(t, err)
		assert.Empty(t, resp.Plans)
		assert.False(t, resp.HasMore)
		assert.Empty(t, resp.NextCursor)
	})

	t.Run("returns all plans", func(t *testing.T) {
		t.Parallel()
		srv := NewPlanServiceServer(&stubPlanService{
			plans: []handlers.Plan{
				{ID: "plan-1", Name: "Basic", Amount: "1000", Currency: "USD", Interval: "month", Description: "Basic plan"},
				{ID: "plan-2", Name: "Pro", Amount: "2000", Currency: "USD", Interval: "month", Description: "Pro plan"},
				{ID: "plan-3", Name: "Enterprise", Amount: "5000", Currency: "USD", Interval: "year", Description: "Enterprise plan"},
			},
		})
		resp, err := srv.ListPlans(context.Background(), &pb.ListPlansRequest{Limit: 10})
		require.NoError(t, err)
		require.Len(t, resp.Plans, 3)
		assert.Equal(t, "plan-1", resp.Plans[0].Id)
		assert.Equal(t, "Basic", resp.Plans[0].Name)
		assert.Equal(t, "1000", resp.Plans[0].Amount)
		assert.Equal(t, "USD", resp.Plans[0].Currency)
		assert.Equal(t, "month", resp.Plans[0].Interval)
		assert.Equal(t, "Basic plan", resp.Plans[0].Description)
		assert.False(t, resp.HasMore)
	})

	t.Run("pagination with limit", func(t *testing.T) {
		t.Parallel()
		srv := NewPlanServiceServer(&stubPlanService{
			plans: []handlers.Plan{
				{ID: "plan-1", Name: "Basic"},
				{ID: "plan-2", Name: "Pro"},
				{ID: "plan-3", Name: "Enterprise"},
				{ID: "plan-4", Name: "Ultimate"},
				{ID: "plan-5", Name: "Premium"},
			},
		})

		// First page with limit 2
		resp, err := srv.ListPlans(context.Background(), &pb.ListPlansRequest{Limit: 2})
		require.NoError(t, err)
		require.Len(t, resp.Plans, 2)
		assert.True(t, resp.HasMore)
		assert.Equal(t, "plan-2", resp.NextCursor)
		assert.Equal(t, "plan-1", resp.Plans[0].Id)
		assert.Equal(t, "plan-2", resp.Plans[1].Id)

		// Second page starting from cursor
		resp, err = srv.ListPlans(context.Background(), &pb.ListPlansRequest{Limit: 2, Cursor: "plan-2"})
		require.NoError(t, err)
		require.Len(t, resp.Plans, 2)
		assert.True(t, resp.HasMore)
		assert.Equal(t, "plan-4", resp.NextCursor)
		assert.Equal(t, "plan-3", resp.Plans[0].Id)
		assert.Equal(t, "plan-4", resp.Plans[1].Id)

		// Third page - last item
		resp, err = srv.ListPlans(context.Background(), &pb.ListPlansRequest{Limit: 2, Cursor: "plan-4"})
		require.NoError(t, err)
		require.Len(t, resp.Plans, 1)
		assert.False(t, resp.HasMore)
		assert.Empty(t, resp.NextCursor)
		assert.Equal(t, "plan-5", resp.Plans[0].Id)
	})

	t.Run("default limit when limit is zero", func(t *testing.T) {
		t.Parallel()
		plans := make([]handlers.Plan, 15)
		for i := 0; i < 15; i++ {
			plans[i] = handlers.Plan{ID: "plan-" + string(rune('0'+i))}
		}
		srv := NewPlanServiceServer(&stubPlanService{plans: plans})
		resp, err := srv.ListPlans(context.Background(), &pb.ListPlansRequest{Limit: 0})
		require.NoError(t, err)
		assert.Len(t, resp.Plans, 10) // default limit
		assert.True(t, resp.HasMore)
	})
}

func TestPlanServiceServer_GetPlan(t *testing.T) {
	t.Parallel()

	t.Run("existing plan", func(t *testing.T) {
		t.Parallel()
		srv := NewPlanServiceServer(&stubPlanService{
			plans: []handlers.Plan{
				{ID: "plan-1", Name: "Basic", Amount: "1000", Currency: "USD"},
				{ID: "plan-2", Name: "Pro", Amount: "2000", Currency: "USD"},
			},
		})
		resp, err := srv.GetPlan(context.Background(), &pb.GetPlanRequest{Id: "plan-1"})
		require.NoError(t, err)
		require.NotNil(t, resp.Plan)
		assert.Equal(t, "plan-1", resp.Plan.Id)
		assert.Equal(t, "Basic", resp.Plan.Name)
	})

	t.Run("non-existent plan", func(t *testing.T) {
		t.Parallel()
		srv := NewPlanServiceServer(&stubPlanService{
			plans: []handlers.Plan{{ID: "plan-1", Name: "Basic"}},
		})
		_, err := srv.GetPlan(context.Background(), &pb.GetPlanRequest{Id: "nonexistent"})
		require.Error(t, err)
	})

	t.Run("empty id", func(t *testing.T) {
		t.Parallel()
		srv := NewPlanServiceServer(&stubPlanService{})
		_, err := srv.GetPlan(context.Background(), &pb.GetPlanRequest{Id: ""})
		require.Error(t, err)
	})
}

func TestPlanServiceServer_ListPlans_ServiceError(t *testing.T) {
	t.Parallel()
	srv := NewPlanServiceServer(&stubPlanService{err: assert.AnError})
	_, err := srv.ListPlans(context.Background(), &pb.ListPlansRequest{Limit: 10})
	require.Error(t, err)
}
