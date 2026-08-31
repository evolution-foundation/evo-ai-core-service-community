-- The old repair stamped a bare model id that retires on 2026-10-23, and it cannot
-- undo it: it only fills an empty model on an agent still typed sequential/parallel/
-- loop, and it persists the coercion to llm. Every user-driven write stores
-- provider/model, so the bare id is what the machine wrote.
DO $$
DECLARE
  was_forced boolean;
  affected integer;
BEGIN
  SELECT relforcerowsecurity INTO STRICT was_forced
  FROM pg_class WHERE oid = 'evo_core_agents'::regclass;

  -- Forced row level security filters even the table owner, and the role that runs
  -- the migrations crosses no tenant policy, so the UPDATE would match nothing while
  -- the migration still reported success. Lifting FORCE is the owner's to do and the
  -- ALTER's lock hides the window; a role that does not own the table fails here.
  PERFORM set_config('row_security', 'off', true);
  IF was_forced THEN
    EXECUTE 'ALTER TABLE evo_core_agents NO FORCE ROW LEVEL SECURITY';
  END IF;

  UPDATE evo_core_agents
  SET model = 'openai/gpt-5.6-luna'
  WHERE model = 'gpt-4.1-nano';
  GET DIAGNOSTICS affected = ROW_COUNT;

  IF was_forced THEN
    EXECUTE 'ALTER TABLE evo_core_agents FORCE ROW LEVEL SECURITY';
  END IF;

  -- LOG, not NOTICE: golang-migrate discards notices, so the count would land nowhere.
  RAISE LOG 'moved % agent(s) off the retired repair model', affected;
END
$$;
