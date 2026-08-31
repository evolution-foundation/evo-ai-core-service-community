-- Deliberately empty. The backfill writes the same value the repair writes today,
-- so nothing distinguishes a backfilled row from an agent legitimately on that
-- model, and reverting would push live agents back onto a retired id.
SELECT 1;
