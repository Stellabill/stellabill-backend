# Repository Unit Tests with SQL Mocking

This document describes the comprehensive repository unit tests implemented for the stellabill-backend project using SQL mocking to verify query correctness and error handling without external database dependencies.

## Overview

The repository layer tests provide comprehensive coverage for:
- Plan repository operations
- Subscription repository operations  
- Outbox repository operations
- Edge cases and error handling scenarios
- Concurrent access patterns
- Null value handling
- Retry logic and deadlock simulation

## Test Architecture

### Dependencies

- **github.com/DATA-DOG/go-sqlmock**: SQL mocking framework for database operations
- **github.com/stretchr/testify**: Assertion and testing utilities
- **github.com/google/uuid**: UUID generation for test data

### Test Structure

```
internal/
├── repositories/
│   ├── plans.go                    # Plan repository implementation
│   ├── plans_test.go               # Plan repository unit tests
│   ├── subscriptions.go            # Subscription repository implementation
│   ├── subscriptions_test.go       # Subscription repository unit tests
│   └── edge_cases_test.go          # Edge cases and error handling tests
└── outbox/
    ├── repository.go               # Outbox repository implementation
    └── repository_test.go         # Outbox repository unit tests
```

## Repository Test Patterns

### 1. Standard CRUD Operations

Each repository follows a consistent testing pattern:

```go
func TestRepository_Method(t *testing.T) {
    db, mock, err := sqlmock.New()
    require.NoError(t, err)
    defer db.Close()
    
    repo := NewRepository(db)
    
    tests := []struct {
        name          string
        input         *InputType
        expectedError string
        setupMock     func()
    }{
        // Test cases...
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            tt.setupMock()
            
            err := repo.Method(tt.input)
            
            if tt.expectedError != "" {
                assert.Error(t, err)
                assert.Contains(t, err.Error(), tt.expectedError)
            } else {
                assert.NoError(t, err)
            }
            
            assert.NoError(t, mock.ExpectationsWereMet())
        })
    }
}
```

### 2. Mock Setup Patterns

#### Successful Operations
```go
mock.ExpectQuery(`INSERT INTO plans`).
    WithArgs(sqlmock.AnyArg(), "Plan Name", "29.99", "USD", "month", nil, "merchant-123", sqlmock.AnyArg(), sqlmock.AnyArg()).
    WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New().String()))
```

#### Error Scenarios
```go
mock.ExpectQuery(`SELECT.*FROM plans WHERE id = \$1`).
    WithArgs("nonexistent").
    WillReturnError(sql.ErrNoRows)
```

#### Database Errors
```go
mock.ExpectExec(`UPDATE plans SET.*`).
    WithArgs(...).
    WillReturnError(fmt.Errorf("database connection failed"))
```

### 3. Null Value Handling

Tests explicitly verify proper handling of nullable database columns:

```go
// Test with null description
rows := sqlmock.NewRows([]string{"id", "name", "description", ...}).
    AddRow("plan-123", "Basic Plan", nil, ...)
mock.ExpectQuery(`SELECT.*`).WillReturnRows(rows)

plan, err := repo.GetByID("plan-123")
assert.NoError(t, err)
assert.Nil(t, plan.Description)
```

### 4. Error Mapping Tests

Comprehensive error scenarios are tested:

- **Connection failures**: Database connection errors
- **Deadlock simulation**: Concurrent update conflicts
- **Constraint violations**: Foreign key and unique constraints
- **Data type mismatches**: Invalid data scanning
- **Permission errors**: Access denied scenarios

## Coverage Requirements

### Plan Repository Tests

- ✅ Create plan with/without description
- ✅ Get plan by ID (found/not found)
- ✅ Get plans by merchant ID with pagination
- ✅ Update plan (success/not found)
- ✅ Delete plan (success/not found)
- ✅ Get active plans by merchant ID
- ✅ Null value handling
- ✅ Scan error handling

### Subscription Repository Tests

- ✅ Create subscription with/without trial period
- ✅ Get subscription by ID (found/not found)
- ✅ Get subscriptions by customer ID with pagination
- ✅ Get subscriptions by merchant ID with pagination
- ✅ Get subscriptions by plan ID with pagination
- ✅ Update subscription (success/not found)
- ✅ Update status (success/not found)
- ✅ Cancel subscription (immediate/period end)
- ✅ Get active subscriptions by merchant ID
- ✅ Get subscriptions due for billing
- ✅ Null value handling for all time fields
- ✅ Scan error handling

### Outbox Repository Tests

- ✅ Store event with/without optional fields
- ✅ Get pending events (pending/failed/retry)
- ✅ Get event by ID (found/not found)
- ✅ Update status with/without error message
- ✅ Mark as processing with race condition protection
- ✅ Increment retry count with backoff
- ✅ Delete completed events
- ✅ Null value handling
- ✅ Scan error handling
- ✅ Retry logic simulation

### Edge Cases Tests

- ✅ Database connection failures
- ✅ Deadlock simulation
- ✅ Concurrent access patterns
- ✅ Large data handling
- ✅ Empty result sets
- ✅ Retry logic with various error types

## Security Considerations

### Input Validation
- All SQL queries use parameterized statements to prevent SQL injection
- Mock expectations verify exact parameter binding
- Tests include malformed data scenarios

### Error Information Leakage
- Error messages are tested to ensure they don't expose sensitive information
- Database errors are wrapped with appropriate error messages
- Stack traces are not exposed in production error responses

### Transaction Safety
- Concurrent access tests verify race condition protection
- Deadlock scenarios are properly handled
- Atomic operations are tested for consistency

## Performance Considerations

### Query Optimization
- Tests verify proper use of indexes through WHERE clauses
- Pagination is tested to prevent large result sets
- LIMIT clauses are properly enforced

### Resource Management
- Database connections are properly closed in tests
- Row iteration errors are handled gracefully
- Memory usage is controlled through proper result set handling

## Running Tests

### Prerequisites
```bash
go get github.com/DATA-DOG/go-sqlmock
go get github.com/stretchr/testify
go get github.com/google/uuid
```

### Execute Tests
```bash
# Run all repository tests
go test ./internal/repositories/...

# Run outbox tests
go test ./internal/outbox/...

# Run with coverage
go test -cover ./internal/repositories/...
go test -cover ./internal/outbox/...

# Run with coverage report
go test -coverprofile=coverage.out ./internal/...
go tool cover -html=coverage.out
```

### Coverage Requirements
- **Minimum coverage**: 95%
- **Target coverage**: 98%+
- **Critical paths**: 100% coverage

## Test Data Management

### Test Isolation
- Each test uses a fresh mock database connection
- Test data is isolated between test cases
- Mock expectations are verified after each test

### Data Generation
- UUIDs are generated for unique identifiers
- Time values use consistent time zones
- Test data follows realistic business constraints

## Continuous Integration

### Test Execution
- Tests run on every pull request
- Coverage thresholds are enforced
- Performance benchmarks are monitored

### Quality Gates
- All tests must pass before merge
- Coverage requirements must be met
- No new test failures introduced

## Best Practices

### Test Organization
- Group related tests in table-driven format
- Use descriptive test names
- Maintain test data consistency

### Mock Usage
- Always verify mock expectations
- Use `sqlmock.AnyArg()` for dynamic values
- Test both success and failure scenarios

### Error Handling
- Test all error paths
- Verify error message content
- Ensure proper resource cleanup

## Future Enhancements

### Additional Test Scenarios
- Performance load testing
- Integration testing with real database
- Chaos engineering scenarios

### Test Utilities
- Common test data builders
- Reusable mock helpers
- Automated test data generation

### Monitoring
- Test execution time tracking
- Coverage trend analysis
- Test stability metrics

## Conclusion

The repository unit tests provide comprehensive coverage of all database operations while ensuring security, performance, and reliability. The SQL mocking approach allows for fast, isolated tests without external dependencies while maintaining high confidence in the correctness of the repository layer.
