-- Currencies are now first-class: a small reference table the ledger owns, so
-- every wallet/entry currency_id resolves to real metadata (code, symbol,
-- decimals) and we can tell test money (is_real=false) from real money
-- (is_real=true). Existing rows already use currency_id 1 as the dev currency.
CREATE TABLE IF NOT EXISTS currencies (
    id         BIGINT      PRIMARY KEY,
    code       TEXT        NOT NULL UNIQUE,   -- short machine code, e.g. DEV, GRAM
    name       TEXT        NOT NULL,          -- display name
    symbol     TEXT        NOT NULL,          -- display symbol/ticker
    decimals   INT         NOT NULL,          -- on-chain precision (info only; storage is NUMERIC(38,18))
    is_real    BOOLEAN     NOT NULL,          -- false = test/dev money, true = backed by a real chain
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Seed the two known currencies. id 1 is the existing dev/test currency; id 2 is
-- Gram (the TON blockchain unit), real money funded only by on-chain deposits.
INSERT INTO currencies (id, code, name, symbol, decimals, is_real) VALUES
    (1, 'DEV',  'Dev Coin', '◈',   18, false),
    (2, 'GRAM', 'Gram',     'TON',  9, true)
ON CONFLICT (id) DO NOTHING;

-- Tie wallet/account currency_id to the reference table. Existing data uses id 1,
-- which the seed above provides, so the FK validates cleanly.
ALTER TABLE accounts
    ADD CONSTRAINT accounts_currency_fk
    FOREIGN KEY (currency_id) REFERENCES currencies (id);
