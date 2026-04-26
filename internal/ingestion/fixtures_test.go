package ingestion

var (
	ValidSubscriptionCreated = `{"idempotency_key":"evt-001","event_type":"contract.created","contract_id":"contract-123","tenant_id":"tenant-456","occurred_at":"2024-01-15T10:30:00Z","sequence_num":1,"payload":{"subscriber_id":"sub-789","plan_id":"plan-basic","amount":"9.99","currency":"USD","interval":"month"}}`

	ValidSubscriptionAmended = `{"idempotency_key":"evt-002","event_type":"contract.amended","contract_id":"contract-123","tenant_id":"tenant-456","occurred_at":"2024-02-15T10:30:00Z","sequence_num":2,"payload":{"plan_id":"plan-premium","amount":"19.99","currency":"USD","interval":"month"}}`

	ValidSubscriptionRenewed = `{"idempotency_key":"evt-003","event_type":"contract.renewed","contract_id":"contract-123","tenant_id":"tenant-456","occurred_at":"2024-03-15T10:30:00Z","sequence_num":3,"payload":{"period_start":"2024-02-15T10:30:00Z","period_end":"2024-03-15T10:30:00Z","amount":"9.99","currency":"USD"}}`

	ValidSubscriptionCancelled = `{"idempotency_key":"evt-004","event_type":"contract.cancelled","contract_id":"contract-123","tenant_id":"tenant-456","occurred_at":"2024-04-01T10:30:00Z","sequence_num":4,"payload":{"reason":"user_request","effective_at":"2024-04-15T10:30:00Z"}}`

	ValidSubscriptionExpired = `{"idempotency_key":"evt-005","event_type":"contract.expired","contract_id":"contract-123","tenant_id":"tenant-456","occurred_at":"2024-04-15T10:30:00Z","sequence_num":5,"payload":{"reason":"failed_to_renew"}}`
)

var (
	MissingIdempotencyKey = `{"event_type":"contract.created","contract_id":"contract-123","tenant_id":"tenant-456","occurred_at":"2024-01-15T10:30:00Z","sequence_num":1,"payload":{}}`

	MissingEventType = `{"idempotency_key":"evt-001","contract_id":"contract-123","tenant_id":"tenant-456","occurred_at":"2024-01-15T10:30:00Z","sequence_num":1,"payload":{}}`

	InvalidEventType = `{"idempotency_key":"evt-001","event_type":"contract.unknown","contract_id":"contract-123","tenant_id":"tenant-456","occurred_at":"2024-01-15T10:30:00Z","sequence_num":1,"payload":{}}`

	MissingContractID = `{"idempotency_key":"evt-001","event_type":"contract.created","tenant_id":"tenant-456","occurred_at":"2024-01-15T10:30:00Z","sequence_num":1,"payload":{}}`

	MissingTenantID = `{"idempotency_key":"evt-001","event_type":"contract.created","contract_id":"contract-123","occurred_at":"2024-01-15T10:30:00Z","sequence_num":1,"payload":{}}`

	MissingOccurredAt = `{"idempotency_key":"evt-001","event_type":"contract.created","contract_id":"contract-123","tenant_id":"tenant-456","sequence_num":1,"payload":{}}`

	InvalidOccurredAt = `{"idempotency_key":"evt-001","event_type":"contract.created","contract_id":"contract-123","tenant_id":"tenant-456","occurred_at":"not-a-date","sequence_num":1,"payload":{}}`

	InvalidPayload = `{"idempotency_key":"evt-001","event_type":"contract.created","contract_id":"contract-123","tenant_id":"tenant-456","occurred_at":"2024-01-15T10:30:00Z","sequence_num":1,"payload":"not-an-object"}`

	NegativeSequence = `{"idempotency_key":"evt-001","event_type":"contract.created","contract_id":"contract-123","tenant_id":"tenant-456","occurred_at":"2024-01-15T10:30:00Z","sequence_num":-1,"payload":{}}`
)