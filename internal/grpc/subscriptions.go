package grpc

import (
	"context"

	"stellarbill-backend/internal/handlers"

	pb "stellarbill-backend/gen/stellabill/v1"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// SubscriptionServiceServer implements the gRPC SubscriptionService.
type SubscriptionServiceServer struct {
	pb.UnimplementedSubscriptionServiceServer
	subscriptionService handlers.SubscriptionService
}

// NewSubscriptionServiceServer creates a new SubscriptionServiceServer.
func NewSubscriptionServiceServer(subscriptionService handlers.SubscriptionService) *SubscriptionServiceServer {
	return &SubscriptionServiceServer{
		subscriptionService: subscriptionService,
	}
}

// ListSubscriptions returns all subscriptions with cursor pagination.
func (s *SubscriptionServiceServer) ListSubscriptions(ctx context.Context, req *pb.ListSubscriptionsRequest) (*pb.ListSubscriptionsResponse, error) {
	limit := int(req.GetLimit())
	if limit <= 0 {
		limit = 10
	}

	subs, err := s.subscriptionService.ListSubscriptions(nil)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list subscriptions: %v", err)
	}

	// Paginate
	total := len(subs)
	start := 0
	cursor := req.GetCursor()
	if cursor != "" {
		for i, s := range subs {
			if s.ID == cursor {
				start = i + 1
				break
			}
		}
	}

	end := start + limit
	if end > total {
		end = total
	}

	pbSubs := make([]*pb.Subscription, 0, len(subs[start:end]))
	for _, s := range subs[start:end] {
		pbSub := &pb.Subscription{
			Id:       s.ID,
			PlanId:   s.PlanID,
			Customer: s.Customer,
			Status:   s.Status,
			Amount:   s.Amount,
			Interval: s.Interval,
		}
		if s.NextBilling != "" {
			pbSub.NextBilling = s.NextBilling
		}
		pbSubs = append(pbSubs, pbSub)
	}

	hasMore := end < total
	nextCursor := ""
	if hasMore && len(pbSubs) > 0 {
		nextCursor = pbSubs[len(pbSubs)-1].Id
	}

	return &pb.ListSubscriptionsResponse{
		Subscriptions: pbSubs,
		NextCursor:    nextCursor,
		HasMore:       hasMore,
	}, nil
}

// GetSubscription returns a single subscription by ID.
func (s *SubscriptionServiceServer) GetSubscription(ctx context.Context, req *pb.GetSubscriptionRequest) (*pb.GetSubscriptionResponse, error) {
	id := req.GetId()
	if id == "" {
		return nil, status.Error(codes.InvalidArgument, "subscription id is required")
	}

	// Use the handler's GetSubscription which we delegate through the service interface.
	sub, err := s.subscriptionService.GetSubscription(nil, id)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "subscription not found: %v", err)
	}

	pbSub := &pb.Subscription{
		Id:       sub.ID,
		PlanId:   sub.PlanID,
		Customer: sub.Customer,
		Status:   sub.Status,
		Amount:   sub.Amount,
		Interval: sub.Interval,
	}
	if sub.NextBilling != "" {
		pbSub.NextBilling = sub.NextBilling
	}

	return &pb.GetSubscriptionResponse{
		Subscription: pbSub,
	}, nil
}

// Ensure compile-time interface compliance.
var _ pb.SubscriptionServiceServer = (*SubscriptionServiceServer)(nil)
