-- Append-only record of integrity decisions (Phase 4 hardening): every
-- owner acknowledgement of a failed verify, with the recorded head (from
-- DATA_DIR/head), the actual chain tip, and the acknowledged problems as JSON.
-- Same protection as auth_log: no UPDATE, no DELETE, no INSERT OR REPLACE.
CREATE TABLE integrity_events (
  id            INTEGER PRIMARY KEY,
  at            TEXT NOT NULL,
  kind          TEXT NOT NULL,
  recorded_head TEXT,
  actual_head   TEXT,
  problems      TEXT NOT NULL
);
CREATE TRIGGER integrity_events_no_update BEFORE UPDATE ON integrity_events
BEGIN SELECT RAISE(ABORT, 'integrity_events is append-only'); END;
CREATE TRIGGER integrity_events_no_delete BEFORE DELETE ON integrity_events
BEGIN SELECT RAISE(ABORT, 'integrity_events is append-only'); END;
CREATE TRIGGER integrity_events_no_replace BEFORE INSERT ON integrity_events
WHEN EXISTS (SELECT 1 FROM integrity_events WHERE id = NEW.id)
BEGIN SELECT RAISE(ABORT, 'integrity_events is append-only'); END;
