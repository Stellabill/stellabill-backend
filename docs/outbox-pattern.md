# Outbox Pattern Implementation

## Overview

This document describes the implementation of the Outbox Pattern for reliable event publication in the Stellabill backend. The outbox pattern ensures that events are reliably published to external systems without losing messages during partial failures.

## Architecture

### Components

1. **Outbox Table**: Database table that stores events to be published
2. **Repository**: Handles database operations for outbox events
3. **Publisher**: Publishes events to external systems (HTTP, console, etc.)
4. **Dispatcher**: Background process that processes pending events
5. **Service**: High-level interface for the outbox system
6. **Manager**: Manages the lifecycle of the outbox system

### Flow

```
Application Logic → Database Transaction → Outbox Table → Dispatcher → Publisher → External System
```

## Database Schema

The outbox table (`outbox_events`) contains:

- `id`: Unique identifier for the event
- `event_type`: Type of the event
- `event_data`: JSON payload of the event
- `aggregate_id` & `aggregate_type`: Optional aggregate information
- `status`: Current status (pending, processing, completed, failed)
- `retry_count`: Number of retry attempts
- `max_retries`: Maximum allowed retries
- `next_retry_at`: When to retry the event
- `error_message`: Last error message
- `timestamps`: Creation and update timestamps
- `version`: Event version for concurrency control

## Configuration

The outbox system is configured via environment variables:

```bash
# Publisher type: console, http, multi
OUTBOX_PUBLISHER_TYPE=console

# HTTP endpoint for HTTP publisher
OUTBOX_HTTP_ENDPOINT=https://events.example.com/webhook

# Polling interval for dispatcher
OUTBOX_POLL_INTERVAL=5s

# Batch size for processing
OUTBOX_BATCH_SIZE=10

# Maximum retry attempts
OUTBOX_MAX_RETRIES=3

# Retry backoff factor (exponential)
OUTBOX_RETRY_BACKOFF_FACTOR=2.0

# Cleanup interval for completed events
OUTBOX_CLEANUP_INTERVAL=1h

# TTL for completed events
OUTBOX_COMPLETED_EVENT_TTL=24h

# Processing timeout per event
OUTBOX_PROCESSING_TIMEOUT=30s
```

## Usage

### Publishing Events

```go
// Simple event publishing
err := outboxService.PublishEvent(ctx, "user.created", userData, nil, nil)

// With aggregate information
userID := "user-123"
userType := "user"
err := outboxService.PublishEvent(ctx, "user.updated", userData, &userID, &userType)

// Using domain events
event := SubscriptionCreated{
    ID:         "sub-123",
    CustomerID: "cust-456",
    PlanID:     "plan-789",
    Status:     "active",
    OccurredAt: time.Now(),
}
err := outboxManager.PublishDomainEvent(ctx, event)
```

### Transactional Publishing

```go
tx, err := db.BeginTx(ctx, nil)
if err != nil {
    return err
}
defer tx.Rollback()

// Update business data
_, err = tx.Exec("UPDATE users SET status = $1 WHERE id = $2", "active", userID)
if err != nil {
    return err
}

// Publish event in same transaction
event, err := outboxService.PublishEventWithTx(tx, "user.activated", userData, &userID, &userType)
if err != nil {
    return err
}

// Commit transaction (both data and event are saved atomically)
return tx.Commit()
```

## API Endpoints

### Health Check
```
GET /api/health
```

Returns system health including outbox status:
```json
{
  "status": "ok",
  "service": "stellarbill-backend",
  "outbox": {
    "pending_events": 0,
    "dispatcher_running": true,
    "database_health": "healthy"
  }
}
```

### Outbox Statistics
```
GET /api/outbox/stats
```

Returns detailed outbox statistics for monitoring.

### Test Event Publishing
```
POST /api/outbox/test?type=custom.event
```

Publishes a test event for development and testing.

## Error Handling and Recovery

### Retry Strategy

The system implements exponential backoff for failed events:

1. **First failure**: Retry after 1 second
2. **Second failure**: Retry after 2 seconds
3. **Third failure**: Retry after 4 seconds
4. **Subsequent failures**: Continue exponential backoff

### Crash Recovery

The system automatically recovers from crashes:

1. **Pending events**: Events stuck in `pending` status are reprocessed
2. **Processing events**: Events stuck in `processing` status timeout and are retried
3. **Failed events**: Events that haven't reached max retries are retried
4. **Completed events**: Old completed events are automatically cleaned up

### Idempotency

The system ensures idempotency through:

1. **Unique event IDs**: Each event has a unique identifier
2. **Status tracking**: Events are marked as `processing` to prevent duplicate processing
3. **Version control**: Event versions prevent concurrent modifications

## Testing

### Unit Tests

Run unit tests:
```bash
go test ./internal/outbox/...
```

### Integration Tests

Run integration tests (requires test database):
```bash
go test ./internal/outbox/... -tags=integration
```

### Test Coverage

Check test coverage:
```bash
go test -cover ./internal/outbox/...
```

## Security Considerations

### Data Protection

1. **Sensitive Data**: Avoid storing sensitive information in event payloads
2. **Encryption**: Use encryption for sensitive event data if necessary
3. **Access Control**: Limit database access to outbox table

### Network Security

1. **HTTPS**: Always use HTTPS for HTTP publishers
2. **Authentication**: Implement proper authentication for external endpoints
3. **Rate Limiting**: Implement rate limiting for event publishing

### Operational Security

1. **Monitoring**: Monitor outbox queue depth and processing rates
2. **Alerting**: Set up alerts for high failure rates or queue buildup
3. **Audit Trail**: Maintain logs of event processing for audit purposes

## Performance Considerations

### Database Optimization

1. **Indexing**: Proper indexes on status, next_retry_at, and aggregate fields
2. **Partitioning**: Consider partitioning by date for high-volume systems
3. **Cleanup**: Regular cleanup of old completed events

### Processing Optimization

1. **Batch Processing**: Process events in batches to reduce database overhead
2. **Parallel Processing**: Configure appropriate batch sizes and polling intervals
3. **Connection Pooling**: Use database connection pooling

### Memory Management

1. **Event Size**: Limit event payload sizes to prevent memory issues
2. **Buffer Management**: Use appropriate buffer sizes for HTTP publishing
3. **Garbage Collection**: Monitor memory usage and adjust as needed

## Monitoring and Observability

### Metrics to Monitor

1. **Queue Depth**: Number of pending events
2. **Processing Rate**: Events processed per second
3. **Error Rate**: Percentage of failed events
4. **Retry Rate**: Percentage of events requiring retries
5. **Processing Latency**: Time from event creation to successful publishing

### Health Checks

1. **Database Health**: Database connectivity and performance
2. **Dispatcher Health**: Dispatcher running status
3. **Publisher Health**: External endpoint availability

### Logging

Key log messages to monitor:

1. Event creation and storage
2. Event processing attempts
3. Retry attempts and failures
4. Cleanup operations
5. System startup and shutdown

## Troubleshooting

### Common Issues

1. **Events Not Processing**: Check dispatcher status and database connectivity
2. **High Failure Rate**: Check external endpoint availability and network connectivity
3. **Queue Buildup**: Check processing capacity and increase batch size or parallelism
4. **Database Performance**: Check query performance and indexing

### Debugging Tools

1. **Event Status Query**: Check individual event status in database
2. **Statistics API**: Use `/api/outbox/stats` for system overview
3. **Test Events**: Use `/api/outbox/test` for manual testing
4. **Log Analysis**: Review dispatcher and publisher logs

## Migration and Deployment

### Database Migration

The system automatically creates the outbox table on startup. For production deployments:

1. Run the migration script manually: `migrations/001_create_outbox_table.sql`
2. Verify table creation and indexes
3. Test with sample events

### Deployment Strategy

1. **Blue-Green Deployment**: Deploy to canary environment first
2. **Rollback Plan**: Have rollback strategy ready
3. **Monitoring**: Set up monitoring before deployment
4. **Testing**: Verify event publishing in production environment

## Future Enhancements

### Planned Features

1. **Event Versioning**: Support for event schema evolution
2. **Dead Letter Queue**: Separate queue for permanently failed events
3. **Event Replay**: Ability to replay events for recovery
4. **Multi-Region Support**: Geo-distributed event publishing
5. **Streaming Integration**: Integration with Kafka, RabbitMQ, etc.

### Performance Improvements

1. **Async Processing**: Fully asynchronous event processing
2. **Caching**: Cache for frequently accessed event data
3. **Compression**: Event payload compression for large events
4. **Batch Publishing**: Batch multiple events to external systems

## Examples

### Example Domain Event

```go
type SubscriptionCreated struct {
    ID         string    `json:"id"`
    CustomerID string    `json:"customer_id"`
    PlanID     string    `json:"plan_id"`
    Status     string    `json:"status"`
    OccurredAt time.Time `json:"occurred_at"`
}

func (e SubscriptionCreated) EventType() string {
    return "subscription.created"
}

func (e SubscriptionCreated) Data() interface{} {
    return e
}

func (e SubscriptionCreated) AggregateID() *string {
    return &e.ID
}

func (e SubscriptionCreated) AggregateType() *string {
    aggregateType := "subscription"
    return &aggregateType
}

func (e SubscriptionCreated) OccurredAt() time.Time {
    return e.OccurredAt
}
```

### Example Usage in Handler

```go
func CreateSubscription(c *gin.Context) {
    // ... business logic ...
    
    // Create subscription in database
    subscription := createSubscriptionInDB(subData)
    
    // Publish event using outbox
    event := SubscriptionCreated{
        ID:         subscription.ID,
        CustomerID: subscription.CustomerID,
        PlanID:     subscription.PlanID,
        Status:     subscription.Status,
        OccurredAt: time.Now(),
    }
    
    err := outboxManager.PublishDomainEvent(c.Request.Context(), event)
    if err != nil {
        // Log error but don't fail the request
        log.Printf("Failed to publish subscription created event: %v", err)
    }
    
    c.JSON(http.StatusCreated, subscription)
}
```

## Conclusion

The outbox pattern implementation provides reliable event publication with built-in retry mechanisms, crash recovery, and comprehensive monitoring. It ensures that events are not lost during system failures and provides a robust foundation for event-driven architecture.
