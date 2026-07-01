-- Double-entry ledger. The journal (entries) is append-only and authoritative;
-- wallet_balances is a materialized running total updated in the SAME
-- transaction as the entries so a balance read never disagrees with the journal.

-- Append-only journal. Each ledger operation writes a balanced set of rows whose
-- signed amounts sum to zero. Never updated or deleted.
CREATE TABLE IF NOT EXISTS entries (
    id          BIGSERIAL   PRIMARY KEY,
    order_id    TEXT        NOT NULL,
    op          TEXT        NOT NULL,   -- FREEZE | CAPTURE | RELEASE
    wallet_id   TEXT        NOT NULL,
    bucket      TEXT        NOT NULL,   -- AVAILABLE | HELD
    currency_id BIGINT      NOT NULL,
    -- amount is a signed NUMERIC: negative debits the bucket, positive credits it.
    amount      NUMERIC(38,18) NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT entries_op_check     CHECK (op IN ('FREEZE','CAPTURE','RELEASE')),
    CONSTRAINT entries_bucket_check CHECK (bucket IN ('AVAILABLE','HELD'))
);

-- Idempotency: at most one application per (order, op). A replayed Freeze/Capture/
-- Release inserts nothing and is reported as a no-op. This is what lets the saga's
-- outbox dispatcher retry safely.
CREATE UNIQUE INDEX IF NOT EXISTS entries_order_op_uniq
    ON entries (order_id, op, wallet_id, bucket);

-- Read an order's full journal in posting order (audit).
CREATE INDEX IF NOT EXISTS entries_order_idx ON entries (order_id, id);

-- Materialized per-(wallet,currency) balances. available/held are the running
-- sums of the AVAILABLE/HELD journal rows, maintained transactionally.
CREATE TABLE IF NOT EXISTS wallet_balances (
    wallet_id   TEXT           NOT NULL,
    currency_id BIGINT         NOT NULL,
    available   NUMERIC(38,18) NOT NULL DEFAULT 0,
    held        NUMERIC(38,18) NOT NULL DEFAULT 0,
    updated_at  TIMESTAMPTZ    NOT NULL DEFAULT now(),

    PRIMARY KEY (wallet_id, currency_id),
    -- A wallet may never go negative in either bucket.
    CONSTRAINT wallet_balances_nonneg CHECK (available >= 0 AND held >= 0)
);
