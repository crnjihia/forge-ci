-- path: migrations/000002_billing_and_secrets.down.sql

DROP TABLE IF EXISTS mpesa_transactions CASCADE;
DROP TABLE IF EXISTS billing_credits CASCADE;
DROP TABLE IF EXISTS artifacts CASCADE;
DROP TABLE IF EXISTS secrets CASCADE;

