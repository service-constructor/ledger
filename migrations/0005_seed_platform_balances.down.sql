-- Remove only the seeded, still-zero platform balance rows. Rows that have
-- accrued fees (available > 0 or held > 0) are real balances and must survive a
-- rollback, so they are left untouched.
DELETE FROM wallet_balances
WHERE wallet_id = 'wlt_platform' AND available = 0 AND held = 0;
