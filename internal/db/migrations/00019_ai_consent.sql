-- +goose Up

-- What a run handed to the AI provider besides the repository's code: the
-- prompt, the error summary and the event with personal data removed.
ALTER TABLE ai_runs ADD COLUMN shared_context text NOT NULL DEFAULT '';

-- Organizations now decide whether their data may go to the AI provider
-- (OrganizationSettings.ai_enabled, off by default). Those that already
-- ran analyses or set up automatic ones keep them.
SELECT set_config('izci.system', 'on', true);
UPDATE organizations SET settings = settings || '{"ai_enabled": true}'::jsonb
WHERE id IN (SELECT org_id FROM ai_runs)
   OR id IN (SELECT org_id FROM projects WHERE settings -> 'ai' ->> 'auto_analyze' = 'AUTO_ANALYZE_NEW_ISSUES');

-- +goose Down
ALTER TABLE ai_runs DROP COLUMN shared_context;
