package repository

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestStatementsRepoCreateInvalidInput(t *testing.T) {
	db, _, _ := sqlmock.New()
	defer db.Close()

	repo := NewStatementsRepo(db)
	id, err := repo.Create(context.Background(), &Statement{TenantID: ""})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
	if id != 0 {
		t.Fatalf("expected id 0, got %d", id)
	}
}

func TestStatementsRepoCreate(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	repo := NewStatementsRepo(db)

	query := regexp.QuoteMeta("INSERT INTO statements (tenant_id, content, created_at, updated_at) VALUES (?, ?, ?, ?) RETURNING id")
	mock.ExpectQuery(query).
		WithArgs("tenant-A", "hello", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(42)))

	id, err := repo.Create(context.Background(), &Statement{TenantID: "tenant-A", Content: "hello"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if id != 42 {
		t.Fatalf("expected id 42, got %d", id)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestStatementsRepoGet(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	repo := NewStatementsRepo(db)
	createdAt := time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC)

	query := regexp.QuoteMeta("SELECT id, tenant_id, content, created_at, updated_at FROM statements WHERE tenant_id = ? AND id = ?")
	mock.ExpectQuery(query).
		WithArgs("tenant-A", 7).
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "content", "created_at", "updated_at"}).
			AddRow(7, "tenant-A", "content", createdAt, createdAt))

	got, err := repo.Get(context.Background(), "tenant-A", 7)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.ID != 7 || got.TenantID != "tenant-A" || got.Content != "content" {
		t.Fatalf("unexpected statement: %+v", got)
	}
}

func TestStatementsRepoGetInvalidInput(t *testing.T) {
	db, _, _ := sqlmock.New()
	defer db.Close()

	repo := NewStatementsRepo(db)
	if _, err := repo.Get(context.Background(), "", 1); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for empty tenant, got %v", err)
	}
	if _, err := repo.Get(context.Background(), "tenant-A", 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for zero id, got %v", err)
	}
}

func TestStatementsRepoGetNotFound(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	repo := NewStatementsRepo(db)

	query := regexp.QuoteMeta("SELECT id, tenant_id, content, created_at, updated_at FROM statements WHERE tenant_id = ? AND id = ?")
	mock.ExpectQuery(query).
		WithArgs("tenant-A", 99).
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "content", "created_at", "updated_at"}))

	_, err := repo.Get(context.Background(), "tenant-A", 99)
	if !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected no-rows/not-found error, got %v", err)
	}
}

func TestStatementsRepoList(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	repo := NewStatementsRepo(db)
	createdAt := time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery("SELECT id, tenant_id, content, created_at, updated_at FROM statements WHERE tenant_id = \\? ORDER BY created_at").
		WithArgs("tenant-A").
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "content", "created_at", "updated_at"}).
			AddRow(1, "tenant-A", "one", createdAt, createdAt).
			AddRow(2, "tenant-A", "two", createdAt, createdAt))

	stmts, err := repo.List(context.Background(), "tenant-A", nil, nil)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(stmts) != 2 {
		t.Fatalf("expected 2 statements, got %d", len(stmts))
	}
	if stmts[0].ID != 1 || stmts[1].ID != 2 {
		t.Fatalf("unexpected statement order: %+v", stmts)
	}
}

func TestStatementsRepoListInvalidInput(t *testing.T) {
	db, _, _ := sqlmock.New()
	defer db.Close()

	repo := NewStatementsRepo(db)
	if _, err := repo.List(context.Background(), "", nil, nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
}

func TestStatementsRepoUpdateNotFound(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	repo := NewStatementsRepo(db)

	mock.ExpectExec(regexp.QuoteMeta("UPDATE statements SET content = ?, updated_at = ? WHERE tenant_id = ? AND id = ?")).
		WithArgs("new", sqlmock.AnyArg(), "tenant-A", 9).
		WillReturnResult(sqlmock.NewResult(0, 0))

	err := repo.Update(context.Background(), "tenant-A", 9, "new")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestStatementsRepoDeleteNotFound(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	repo := NewStatementsRepo(db)

	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM statements WHERE tenant_id = ? AND id = ?")).
		WithArgs("tenant-A", 9).
		WillReturnResult(sqlmock.NewResult(0, 0))

	err := repo.Delete(context.Background(), "tenant-A", 9)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}