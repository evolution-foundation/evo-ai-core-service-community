-- The old repair stamped a bare model id onto every agent it coerced to llm, and
-- that id retires on 2026-10-23. The repair cannot undo it: it only fills an empty
-- model on an agent still typed sequential/parallel/loop, and it persists the
-- coercion to llm, so those rows never reach it a second time.
--
-- Every user-driven write stores provider/model, so the bare id is what the machine
-- wrote. The new value matches the constant the repair stamps today, and it routes
-- on both paths: straight to OpenAI, or as openrouter/openai/gpt-5.6-luna once the
-- provider normalizer prefixes it.
DO $$
DECLARE
  affected integer;
BEGIN
  -- evo_core_agents forces row level security, so a role that cannot cross
  -- tenant_isolation updates nothing while the migration still reports success.
  -- Turning row security off here turns that silent no-op into a hard failure.
  PERFORM set_config('row_security', 'off', true);

  UPDATE evo_core_agents
  SET model = 'openai/gpt-5.6-luna'
  WHERE model = 'gpt-4.1-nano';

  GET DIAGNOSTICS affected = ROW_COUNT;
  RAISE NOTICE 'moved % agent(s) off the retired repair model', affected;
END
$$;
