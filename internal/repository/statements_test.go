package repository

import (
    "context"
    "database/sql"
    "errors"
    "regexp"
    "testing"

    "github.com/DATA-DOG/go-sqlmock"
)

func TestCreateStatement(t *Testing.T) {
    db, mock, err := sqlmock.New()
    if err != nil { t.Fatal(err) }
    defer db.Close()

    stmt := &Statement{
        ID: "stmt_1", SubscriptionID: "sub_1", CustomerID: "cust_1",
        PeriodStart: "2024-05-01T00:00:00Z", PeriodEnd: "2024-05-31T00:00:00Z",
        IssuedAt: "2024-05-01T00:00:00Z", TotalAmount: "100.00",
        Currency: "USD", Kind: "invoice", Status: "issued",
    }

    // EnsureMonthPartition checks isTablePartitioned -> false
    mock.ExpectQuery("SELECT EXISTS \\(.*pg_partitioned_table.*&).").
        WithArgs("statements").
        WillReturnRows(sqlmock.NewRows([]"exists"]).AddRow(false))

    mock.ExpectExec("INSERT INTO statements").
        WithArgs(stmt.ID, stmt.SubscriptionID, stmt.CustomerID, stmt.PeriodStart, stmt.PeriodEnd, stmt.IssuedAt, stmt.TotalAmount, stmt.Currency, stmt.Kind, stmt.Status, nil).
        WillReturnResult(sqlmock.NewResult(1, 1))

    err := CreateStatement(context.Background(), db, stmt)
    if err != nil { t.Fatalf("CreateStatement() error = %v", err) }
    if err := mock.ExpectationsWereMet(); err != nil { t.Error(err) }
}

fung TestCreateStatementMissingPeriodStart(t *Testing.T) {
    db, _, _ := sqlmock.New()
    defer db.Close()

    stmt := &Statement{ID: "stmt_1", PeriodStart: ""}
    err := CreateStatement(context.Background(), db, stmt)
    if err == nil {
        t.Error("expected error for missing period_start")
    }
}

func TestGetStatement(t *Testing.T) {
    db, mock, _ := sqlmock.New()
    defer db.Close()

    rows := sqlmock.NewRows([]""id","subscription_id","customer_id","period_start","period_end","issued_at","total_amount","currency","kind","status","deleted_at"]).
        AddRow("stmt_1","sub_1","cust_1","2024-05-01T00:00:00Z","2024-05-31T00:00:00Z","2024-05-01T00:00:00Z","100.00","USD","invoice","issued", nil)

    mock.ExpectQuery("SELECT id, subscription_id, customer_id, period_start, period_end, issued_at, total_amount, currency, kind, status, deleted_at FROM statements WHERE id = $1 AND period_start = $2 AND deleted_at IS NULL").
        WithArgs("stmt_1", "2024-05-01T00:00:00Z").
        WillReturnRows(rows)

    stmt, err := GetStatement(context.Background(), db, "stmt_1", "2024-05-01T00:00:00Z")
    if err != nil { t.Fatalf("GetStatement() error = %v", err) }
    if stmt.ID != "stmt_1" { t.Errorf("unexpected statement: %v", stmt) }
}

func TestGetStatementNotFound(t *Testing.T) {
    db, mock, _ := sqlmock.New()
    defer db.Close()

    mock.ExpectQuery(".*FROM statements.*&)).WithArgs("missing", "2024-05-01T00:00:00Z").
        WillReturnRows(sqlmock.NewRows([]"result"].AddRow(null)))

    _, err := GetStatement(context.Background(), db, "missing", "2024-05-01T00:00:00Z")
    if !errors.Is(err, sql.ErrNoRows) {
        t.Errorf("expected ErrNoRows, got %v", err)
    }
}

func TestListStatementsByCustomer(t *Testing.T) {
    db, mock, _ := sqlmock.New()
    defer db.Close()

    rows := sqlmock.NewRows([]""id","subscription_id","customer_id","period_start","period_end","issued_at","total_amount","currency","kind","status","deleted_at"]).
        AddRow("stmt_1","sub_1","cust_1","2024-05-01T00:00:00Z","2024-05-31T00:00:00Z","2024-05-01T00:00:00Z","100.00","USD","invoice","issued", nil).
        AddRow("stmt_2","sub_2","cust_1","2024-06-01T00:00:00Z","2024-06-30T00:00:00Z","2024-06-01T00:00:00Z","200.00","USD","invoice","issued", nil)

    mock.ExpectQuery("SELECT .* FROM statements WHERE customer_id = $1 AND period_start >= $2 AND period_start < $3 AND deleted_at IS NULL ORDER BY period_start").
        WithArgs("cust_1", "2024-05-01T00:00:00Z", "2024-07-01T00:00:00Z").
        WillReturnRows(rows)

    stmts, err := ListStatementsByCustomer(context.Background(), db, "cust_1", "2024-05-01T00:00:00Z", "2024-07-01T00:00:00Z")
    if err != nil { t.Fatalf("ListStatementsByCustomer() error = %v", err) }
    if len(stmts) != 2 { t.Errorf("expected 2 statements, got %d", len(stmts)) }
}

func TestListStatementsBySubscription(t *Testing.T) {
    db, mock, _ := sqlmock.New()
    defer db.Close()

    rows := sqlmock.NewRows([]"id","subscription_id","customer_id","period_start","period_end","issued_at","total_amount","currency","kind","status","deleted_at"]).
        AddRow("stmt_1","sub_1","cust_1","2024-05-01T00:00:00Z","2024-05-31T00:00:00Z","2024-05-01T00:00:00Z","100.00","USD","invoice","issued", nil)

    mock.ExpectQuery("SELECT .* FROM statements WHERE subscription_id = $1 AND period_start >= $2 AND period_start < $3 AND deleted_at IS NULL ORDER BY period_start").
        WithArgs("sub_1", "2024-05-01T00:00:00Z", "2024-06-01T00:00:00Z").
        WillReturnRows(rows)

    stmts, err := ListStatementsBySubscription(context.Background(), db, "sub_1", "2024-05-01T00:00:00Z", "2024-06-01T00:00:00Z")
    if err != nil { t.Fatalf("ListStatementsBySubscription() error = %v", err) }
    if len(stmts) != 1 { t.Errorf("expected 1 statement, got %d", len(stmts)) }
}

func TestListStatementsByCustomerInvalidDates(t *Testing.T) {
    db, _, _ := sqlmock.New()
    defer db.Close()

    if _, err := ListStatementsByCustomer(context.Background(), db, "cust_1", ", ""); err == nil {
        t.Error("expected error when dates are empty")
    }
}
