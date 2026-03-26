-- 0001_init.up.sql
-- Initial schema for Stellabill.

CREATE TABLE IF NOT EXISTS plans (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  amount_cents BIGINT NOT NULL,
  currency TEXT NOT NULL,
  interval TEXT NOT NULL,
  description TEXT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS subscriptions (
  id TEXT PRIMARY KEY,
  plan_id TEXT NOT NULL REFERENCES plans(id) ON DELETE RESTRICT,
  customer TEXT NOT NULL,
  status TEXT NOT NULL,
  amount_cents BIGINT NOT NULL,
  interval TEXT NOT NULL,
  next_billing TIMESTAMPTZ NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

