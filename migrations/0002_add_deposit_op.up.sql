-- Deposit is the external-funding entry point: money enters the system from an
-- on-chain transfer. It is a single credit to a wallet's AVAILABLE bucket with
-- no paired debit (the counterparty is the outside world), so unlike the saga
-- ops a DEPOSIT set does not sum to zero. Widen the op CHECK to allow it.
ALTER TABLE entries DROP CONSTRAINT entries_op_check;
ALTER TABLE entries ADD CONSTRAINT entries_op_check
    CHECK (op IN ('FREEZE','CAPTURE','RELEASE','DEPOSIT'));
