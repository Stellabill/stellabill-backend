# Database Indexing Notes

## Subscriptions

Query:
SELECT * FROM subscriptions
WHERE customer = $1 AND status = $2;

Index:
idx_subscriptions_customer_status

Expected:
Index Scan instead of Seq Scan

---

## Billing Queries

Query:
SELECT * FROM subscriptions
ORDER BY next_billing;

Index:
idx_subscriptions_next_billing

---

## Statements

Query:
SELECT * FROM statements
WHERE subscription_id = $1
ORDER BY created_at DESC;

Index:
idx_statements_subscription_created

---

## Security Consideration

Indexes are designed to:
- Avoid full table scans (DoS vector)
- Not expose sensitive fields
- Only include non-sensitive query columns