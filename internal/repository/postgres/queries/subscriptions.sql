-- name: FindSubscriptionByID :one
SELECT id, plan_id, tenant_id, customer_id, status, amount, currency, interval,
       next_billing, updated_at, version, deleted_at
FROM subscriptions
WHERE id = $1;

-- name: FindSubscriptionByIDAndTenant :one
SELECT id, plan_id, tenant_id, customer_id, status, amount, currency, interval,
       next_billing, updated_at, version, deleted_at
FROM subscriptions
WHERE id = $1 AND tenant_id = $2;

-- name: ListSubscriptionsByTenant :many
SELECT id, plan_id, tenant_id, customer_id, status, amount, currency, interval,
       next_billing, updated_at, version, deleted_at
FROM subscriptions
WHERE tenant_id = $1;

-- name: UpdateSubscriptionStatus :execrows
UPDATE subscriptions
SET status = $1, version = version + 1
WHERE id = $2 AND tenant_id = $3;

-- name: UpdateSubscription :execrows
UPDATE subscriptions
SET plan_id = $1, status = $2, amount = $3, currency = $4, interval = $5, next_billing = $6, version = version + 1
WHERE id = $7 AND tenant_id = $8 AND version = $9;

-- name: DeleteSubscription :execrows
DELETE FROM subscriptions
WHERE id = $1 AND tenant_id = $2 AND version = $3;
