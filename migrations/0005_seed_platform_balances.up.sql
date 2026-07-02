-- The platform fee wallet accrues capture fees, one balance per currency. A
-- balance row is otherwise created lazily on the first capture in that currency,
-- so a currency that has never been captured (e.g. GRAM before its first paid
-- order) has no platform balance row at all — the fee account is invisible.
--
-- Seed a zero balance for the platform wallet in every known currency so the
-- per-currency fee account exists from the start. INSERT ... SELECT over the
-- currencies catalog covers both current currencies and any added by an earlier
-- currencies seed; ON CONFLICT keeps it idempotent and preserves accrued fees.
--
-- 'wlt_platform' is the default PLATFORM_WALLET (see internal/config). If that
-- env is overridden, seed that wallet manually — this migration seeds the default.
INSERT INTO wallet_balances (wallet_id, currency_id, available, held)
SELECT 'wlt_platform', c.id, 0, 0
FROM currencies c
ON CONFLICT (wallet_id, currency_id) DO NOTHING;
