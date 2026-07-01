-- Client accounts. The ledger owns each client's wallet AND the on-chain deposit
-- routing: in the demo all accounts share one TON address but each gets a unique
-- memo tag, so an incoming transfer is matched to an account by its memo.
CREATE TABLE IF NOT EXISTS accounts (
    wallet_id   TEXT        PRIMARY KEY,
    user_id     TEXT        NOT NULL,
    ton_address TEXT        NOT NULL,
    memo        TEXT        NOT NULL,
    currency_id BIGINT      NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL
);

-- One account per (user, currency): CreateAccount is idempotent on this.
CREATE UNIQUE INDEX IF NOT EXISTS accounts_user_currency_uniq
    ON accounts (user_id, currency_id);

-- Deposits are routed by memo, so it must be globally unique and fast to look up.
CREATE UNIQUE INDEX IF NOT EXISTS accounts_memo_uniq ON accounts (memo);
