-- name: FindPlanByID :one
SELECT id, tenant_id, name, amount_cents::text, currency, interval, description, updated_at, version
FROM plans
WHERE id = $1;

-- name: ListPlans :many
SELECT id, tenant_id, name, amount_cents::text, currency, interval, description, updated_at, version
FROM plans
ORDER BY name, id;

-- name: UpdatePlan :execrows
UPDATE plans
SET name = $1, description = $2, version = version + 1
WHERE id = $3 AND version = $4;

-- name: DeletePlan :execrows
DELETE FROM plans
WHERE id = $1 AND version = $2;
