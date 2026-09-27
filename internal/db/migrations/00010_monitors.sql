-- +goose Up

-- Cron monitors: scheduled jobs (reports, collections, imports) that report
-- when they run. A monitor belongs to a project and is identified by its
-- slug there, the way Sentry SDKs name it (monitor_slug).
CREATE TABLE monitors (
  id                 uuid PRIMARY KEY DEFAULT uuidv7(),
  org_id             uuid NOT NULL,
  project_id         uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
  slug               text NOT NULL,
  name               text NOT NULL,
  -- "crontab" or "interval".
  schedule_type      text NOT NULL CHECK (schedule_type IN ('crontab', 'interval')),
  -- Crontab expression, e.g. "0 3 * * *" (schedule_type = crontab).
  schedule_crontab   text NOT NULL DEFAULT '',
  -- Every schedule_interval schedule_units (schedule_type = interval).
  schedule_interval  integer NOT NULL DEFAULT 0,
  schedule_unit      text NOT NULL DEFAULT '',
  -- IANA time zone the crontab is read in, e.g. "Europe/Istanbul".
  timezone           text NOT NULL DEFAULT 'UTC',
  -- Minutes a check-in may be late before the run counts as missed.
  checkin_margin     integer NOT NULL DEFAULT 1,
  -- Minutes a run may stay in progress before it counts as timed out.
  max_runtime        integer NOT NULL DEFAULT 30,
  -- Consecutive failed runs that open an issue.
  failure_threshold  integer NOT NULL DEFAULT 1,
  -- Consecutive successful runs that resolve it again.
  recovery_threshold integer NOT NULL DEFAULT 1,
  -- A paused monitor accepts check-ins but raises nothing.
  paused             boolean NOT NULL DEFAULT false,
  created_at         timestamptz NOT NULL DEFAULT now(),
  updated_at         timestamptz NOT NULL DEFAULT now(),
  UNIQUE (project_id, slug)
);
CREATE INDEX monitors_org_idx ON monitors (org_id, name);
SELECT izci_enable_tenant_rls('monitors');

-- State of a monitor in one environment. Rows appear with the first
-- check-in from that environment; until then nothing can be missed.
CREATE TABLE monitor_environments (
  id                  uuid PRIMARY KEY DEFAULT uuidv7(),
  org_id              uuid NOT NULL,
  monitor_id          uuid NOT NULL REFERENCES monitors (id) ON DELETE CASCADE,
  environment         text NOT NULL,
  -- Outcome of the last finished run: "ok", "error", "missed" or "timeout";
  -- "active" until the first run finishes.
  status              text NOT NULL DEFAULT 'active',
  last_checkin_at     timestamptz,
  -- When the next run is expected, and the latest it may check in.
  next_checkin_at     timestamptz,
  next_checkin_latest timestamptz,
  -- Consecutive failed runs, and successful runs since an issue opened.
  failures            integer NOT NULL DEFAULT 0,
  successes           integer NOT NULL DEFAULT 0,
  -- The open issue raised for this monitor and environment, if any.
  issue_id            uuid REFERENCES issues (id) ON DELETE SET NULL,
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  UNIQUE (monitor_id, environment)
);
CREATE INDEX monitor_environments_due_idx ON monitor_environments (next_checkin_latest)
  WHERE next_checkin_latest IS NOT NULL;
SELECT izci_enable_tenant_rls('monitor_environments');

-- Runs of a monitor: check-ins sent by the job, and the missed and timed
-- out runs izci records itself.
CREATE TABLE monitor_checkins (
  id          uuid PRIMARY KEY DEFAULT uuidv7(),
  org_id      uuid NOT NULL,
  monitor_id  uuid NOT NULL REFERENCES monitors (id) ON DELETE CASCADE,
  environment text NOT NULL,
  -- Id the job gave the run (Sentry check_in_id); a later check-in with the
  -- same id finishes it.
  check_in_id uuid NOT NULL,
  -- "in_progress", "ok", "error", "missed" or "timeout".
  status      text NOT NULL,
  duration_ms integer,
  release     text NOT NULL DEFAULT '',
  -- The scheduled time the run answers, when known.
  expected_at timestamptz,
  trace_id    text NOT NULL DEFAULT '',
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  UNIQUE (monitor_id, check_in_id)
);
CREATE INDEX monitor_checkins_monitor_idx ON monitor_checkins (monitor_id, created_at DESC);
CREATE INDEX monitor_checkins_open_idx ON monitor_checkins (created_at) WHERE status = 'in_progress';
CREATE INDEX monitor_checkins_created_idx ON monitor_checkins (created_at);
SELECT izci_enable_tenant_rls('monitor_checkins');

-- +goose Down
DROP TABLE monitor_checkins;
DROP TABLE monitor_environments;
DROP TABLE monitors;
