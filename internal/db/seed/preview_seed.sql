CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

INSERT INTO customers (id, email, name, created_at) VALUES 
  ('usr_00000000000000000000000001', 'preview-admin@stellabill.dev', 'Preview Admin', NOW()),
  ('usr_00000000000000000000000002', 'test-customer@example.com', 'Test Customer', NOW());

INSERT INTO plans (id, name, price, interval) VALUES 
  ('pln_00000000000000000000000001', 'Pro Tier', 2900, 'monthly');

INSERT INTO subscriptions (id, customer_id, plan_id, status) VALUES 
  ('sub_00000000000000000000000001', 'usr_00000000000000000000000002', 'pln_00000000000000000000000001', 'active');