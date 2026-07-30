package grpc

import (
	"context"

	"stellarbill-backend/internal/handlers"

	pb "stellarbill-backend/gen/stellabill/v1"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// PlanServiceServer implements the gRPC PlanService.
type PlanServiceServer struct {
	pb.UnimplementedPlanServiceServer
	planService handlers.PlanService
}

// NewPlanServiceServer creates a new PlanServiceServer.
func NewPlanServiceServer(planService handlers.PlanService) *PlanServiceServer {
	return &PlanServiceServer{
		planService: planService,
	}
}

// ListPlans returns all available plans with cursor pagination.
func (s *PlanServiceServer) ListPlans(ctx context.Context, req *pb.ListPlansRequest) (*pb.ListPlansResponse, error) {
	limit := int(req.GetLimit())
	if limit <= 0 {
		limit = 10
	}

	// Use the mock-compatible handler approach.
	// Build a minimal request context for the PlanService.
	plans, err := s.planService.ListPlans(nil)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list plans: %v", err)
	}

	// Paginate
	total := len(plans)
	start := 0
	cursor := req.GetCursor()
	if cursor != "" {
		// Simple cursor lookup by plan ID
		for i, p := range plans {
			if p.ID == cursor {
				start = i + 1
				break
			}
		}
	}

	end := start + limit
	if end > total {
		end = total
	}

	pbPlans := make([]*pb.Plan, 0, len(plans[start:end]))
	for _, p := range plans[start:end] {
		pbPlans = append(pbPlans, &pb.Plan{
			Id:          p.ID,
			Name:        p.Name,
			Amount:      p.Amount,
			Currency:    p.Currency,
			Interval:    p.Interval,
			Description: p.Description,
		})
	}

	hasMore := end < total
	nextCursor := ""
	if hasMore && len(pbPlans) > 0 {
		nextCursor = pbPlans[len(pbPlans)-1].Id
	}

	return &pb.ListPlansResponse{
		Plans:      pbPlans,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	}, nil
}

// GetPlan returns a single plan by ID.
func (s *PlanServiceServer) GetPlan(ctx context.Context, req *pb.GetPlanRequest) (*pb.GetPlanResponse, error) {
	id := req.GetId()
	if id == "" {
		return nil, status.Error(codes.InvalidArgument, "plan id is required")
	}

	plans, err := s.planService.ListPlans(nil)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get plan: %v", err)
	}

	for _, p := range plans {
		if p.ID == id {
			return &pb.GetPlanResponse{
				Plan: &pb.Plan{
					Id:          p.ID,
					Name:        p.Name,
					Amount:      p.Amount,
					Currency:    p.Currency,
					Interval:    p.Interval,
					Description: p.Description,
				},
			}, nil
		}
	}

	return nil, status.Error(codes.NotFound, "plan not found")
}

// Ensure compile-time interface compliance.
var _ pb.PlanServiceServer = (*PlanServiceServer)(nil)
