-- path: migrations/000001_init_schema.down.sql

DROP TABLE IF EXISTS logs CASCADE;
DROP TABLE IF EXISTS steps CASCADE;
DROP TABLE IF EXISTS runs CASCADE;

