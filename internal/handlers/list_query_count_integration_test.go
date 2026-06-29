//go:build integration

package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"stellarbill-backend/internal/repository"
	"stellarbill-backend/internal/service"
	"stellarbill-backend/internal/testutil"
	"stellarbill-backend/internal/testutil/qcount"
)

func TestIntegration_ListHandlers_QueryCountIsResultSizeInvariant(t *testing.T) {
	pool, probe := newQueryCountPool(t)

	t.Run("ListSubscriptions", func(t *testing.T) {
		small := exerciseListSubscriptions(t, pool, probe, 1)
		large := exerciseListSubscriptions(t, pool, probe, 25)

		require.NoError(t, qcount.CheckResultSizeInvariant(small, large, 0))
	})

	t.Run("ListStatements", func(t *testing.T) {
		small := exerciseListStatements(t, pool, probe, 1, "")
		large := exerciseListStatements(t, pool, probe, 25, "")

		require.NoError(t, qcount.CheckResultSizeInvariant(small, large, 0))
	})

	t.Run("ListStatements fixed filter expansion", func(t *testing.T) {
		unfiltered := exerciseListStatements(t, pool, probe, 1, "")
		filteredSmall := exerciseListStatements(t, pool, probe, 1, "invoice")
		filteredLarge := exerciseListStatements(t, pool, probe, 25, "invoice")

		// Filter validation has a fixed one-query cost, but increasing the
		// filtered result set must not add any further queries.
		require.NoError(t, qcount.CheckResultSizeInvariant(unfiltered, filteredLarge, 1))
		require.NoError(t, qcount.CheckResultSizeInvariant(filteredSmall, filteredLarge, 0))
	})
}

func newQueryCountPool(t *testing.T) (*pgxpool.Pool, *qcount.Probe) {
	t.Helper()

	ctx := context.Background()
	container, err := testutil.StartPostgresContainer(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupContext, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		require.NoError(t, container.Teardown(cleanupContext))
	})

	pool, probe, err := qcount.NewPool(ctx, container.DSN)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool, probe
}

func exerciseListSubscriptions(
	t *testing.T,
	pool *pgxpool.Pool,
	probe *qcount.Probe,
	size int,
) qcount.Sample {
	t.Helper()

	router := gin.New()
	handler := NewHandler(nil, &queryCountSubscriptionService{pool: pool, size: size})
	router.GET("/subscriptions", handler.ListSubscriptions)

	request := httptest.NewRequest(http.MethodGet, "/subscriptions?limit=100", nil)
	requestContext, counter := probe.Track(request.Context())
	request = request.WithContext(requestContext)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())

	var body struct {
		Subscriptions []Subscription `json:"subscriptions"`
	}
	require.NoError(t, json.NewDecoder(response.Body).Decode(&body))
	require.Len(t, body.Subscriptions, size)

	return qcount.NewSample(fmt.Sprintf("ListSubscriptions[%d]", size), len(body.Subscriptions), counter)
}

func exerciseListStatements(
	t *testing.T,
	pool *pgxpool.Pool,
	probe *qcount.Probe,
	size int,
	kind string,
) qcount.Sample {
	t.Helper()

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("caller_id", "customer-1")
		c.Set("roles", []string{"subscriber"})
		c.Next()
	})
	router.GET("/statements", NewListStatementsHandler(service.NewStatementService(nil, &queryCountStatementRepository{
		pool: pool,
		size: size,
	})))

	path := "/statements?customer_id=customer-1&limit=200"
	if kind != "" {
		path += "&kind=" + kind
	}
	request := httptest.NewRequest(http.MethodGet, path, nil)
	requestContext, counter := probe.Track(request.Context())
	request = request.WithContext(requestContext)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())

	var body struct {
		Statements []*service.StatementDetail `json:"statements"`
	}
	require.NoError(t, json.NewDecoder(response.Body).Decode(&body))
	require.Len(t, body.Statements, size)

	label := fmt.Sprintf("ListStatements[%d]", size)
	if kind != "" {
		label += "[kind]"
	}
	return qcount.NewSample(label, len(body.Statements), counter)
}

type queryCountSubscriptionService struct {
	pool *pgxpool.Pool
	size int
}

func (s *queryCountSubscriptionService) ListSubscriptions(c *gin.Context) ([]Subscription, error) {
	rows, err := s.pool.Query(
		c.Request.Context(),
		`SELECT generate_series(1, $1)::text`,
		s.size,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	subscriptions := make([]Subscription, 0, s.size)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		subscriptions = append(subscriptions, Subscription{ID: id})
	}
	return subscriptions, rows.Err()
}

func (s *queryCountSubscriptionService) GetSubscription(*gin.Context, string) (*Subscription, error) {
	return nil, repository.ErrNotFound
}

type queryCountStatementRepository struct {
	pool *pgxpool.Pool
	size int
}

func (r *queryCountStatementRepository) FindByID(context.Context, string) (*repository.StatementRow, error) {
	return nil, repository.ErrNotFound
}

func (r *queryCountStatementRepository) ListByCustomerID(
	ctx context.Context,
	_ string,
	query repository.StatementQuery,
) ([]*repository.StatementRow, int, error) {
	if query.Kind != "" {
		if _, err := r.pool.Exec(ctx, `SELECT $1::text`, query.Kind); err != nil {
			return nil, 0, err
		}
	}

	rows, err := r.pool.Query(ctx, `SELECT generate_series(1, $1)::text`, r.size)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	statements := make([]*repository.StatementRow, 0, r.size)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, 0, err
		}
		statements = append(statements, &repository.StatementRow{ID: id})
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return statements, len(statements), nil
}
