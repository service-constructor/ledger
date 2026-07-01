ALTER TABLE entries DROP CONSTRAINT entries_op_check;
ALTER TABLE entries ADD CONSTRAINT entries_op_check
    CHECK (op IN ('FREEZE','CAPTURE','RELEASE'));
