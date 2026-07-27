package handlers

import (
	"context"
	"net/http"
	"testing"

	"stellarbill-backend/internal/repository"
	"stellarbill-backend/internal/service"
)

type captureStatementService struct {
	query repository.StatementQuery
}

func (s *captureStatementService) ListByCustomer(
	_ context.Context,
	callerID string,
	roles []string,
	customerID string,
	q repository.StatementQuery,
) (*service.ListStatementsDetail, int, []string, error) {
	s.query = q
	return &service.ListStatementsDetail{Statements: []*service.StatementDetail{}}, 0, nil, nil
}

func (s *captureStatementService) GetDetail(
	_ context.Context,
	callerID string,
	roles []string,
	statementID string,
) (*service.StatementDetail, []string, error) {
	return nil, nil, nil
}

func TestListStatements_RSQLFilterRejectedWhenInvalid(t *testing.T) {
	svc := &captureStatementService{}
	h := NewListStatementsHandler(svc)
	r := withAuth(http.MethodGet, "/api/v1/statements", "cust-1", []string{"customer"}, h)
	w := do(r, http.MethodGet, "/api/v1/statements?customer_id=cust-1&filter=amount=foo=100")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestListStatements_RSQLFilterPassedThroughOnSuccess(t *testing.T) {
	svc := &captureStatementService{}
	h := NewListStatementsHandler(svc)
	r := withAuth(http.MethodGet, "/api/v1/statements", "cust-1", []string{"customer"}, h)
	w := do(r, http.MethodGet, "/api/v1/statements?customer_id=cust-1&filter=amount=gt=100;status=in=(open,paid)")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if svc.query.Filter == nil {
		t.Fatal("expected filter to be attached to statement query")
	}
}
