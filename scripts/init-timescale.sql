-- Enable TimescaleDB extension
CREATE EXTENSION IF NOT EXISTS timescaledb CASCADE;

-- Enable UUID generation
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Additional extensions for analytics
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
