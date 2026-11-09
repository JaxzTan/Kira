-- Kira schema v1 (TRD §4). Owned by kira-ledger.
-- Money is integer sen. The entries and auth_log tables are append-only:
-- the triggers below refuse UPDATE and DELETE from any client, not just the app.

CREATE TABLE categories (
  id          INTEGER PRIMARY KEY,
  name        TEXT NOT NULL UNIQUE COLLATE NOCASE,
  color       TEXT,
  icon        TEXT,
  archived_at TEXT
);

CREATE TABLE entries (
  id             INTEGER PRIMARY KEY,                  -- monotonic, = chain position (assigned by ledger.Service)
  kind           TEXT NOT NULL CHECK (kind IN ('create','amend','void')),
  ref_id         INTEGER REFERENCES entries(id),       -- amend/void point at the ORIGINAL create row
  direction      TEXT NOT NULL DEFAULT 'expense' CHECK (direction IN ('expense','income')),
  occurred_on    TEXT,                                 -- YYYY-MM-DD (null for void)
  amount_sen     INTEGER,                              -- null for void
  currency       TEXT NOT NULL DEFAULT 'MYR',
  category_id    INTEGER REFERENCES categories(id),
  payee          TEXT,
  method         TEXT,
  note           TEXT,
  void_reason    TEXT,
  receipt_sha256 TEXT,
  created_at     TEXT NOT NULL,                        -- server UTC timestamp, RFC 3339 seconds
  prev_hash      TEXT NOT NULL,
  hash           TEXT NOT NULL UNIQUE,
  CHECK ((kind = 'create' AND ref_id IS NULL) OR (kind <> 'create' AND ref_id IS NOT NULL)),
  CHECK (kind = 'void' OR (amount_sen IS NOT NULL AND amount_sen > 0 AND amount_sen < 1000000000))
);

CREATE INDEX entries_ref ON entries(ref_id, kind);

CREATE TRIGGER entries_no_update BEFORE UPDATE ON entries
BEGIN SELECT RAISE(ABORT, 'ledger is append-only'); END;
CREATE TRIGGER entries_no_delete BEFORE DELETE ON entries
BEGIN SELECT RAISE(ABORT, 'ledger is append-only'); END;
-- INSERT OR REPLACE deletes the old row WITHOUT firing delete triggers
-- (unless recursive_triggers is on), so refuse any insert that would collide.
CREATE TRIGGER entries_no_replace BEFORE INSERT ON entries
WHEN EXISTS (SELECT 1 FROM entries WHERE id = NEW.id OR hash = NEW.hash)
BEGIN SELECT RAISE(ABORT, 'ledger is append-only'); END;

-- Sessions are deletable (logout / expiry). email + google_sub let the
-- middleware re-check the allow-list on every request (TR-3).
CREATE TABLE sessions (
  token_sha256 TEXT PRIMARY KEY,
  email        TEXT NOT NULL,
  google_sub   TEXT NOT NULL,
  created_at   TEXT,
  expires_at   TEXT,
  user_agent   TEXT
);
CREATE INDEX sessions_expires ON sessions(expires_at);

-- Single row, insert-once: the pinned Google sub (CLAUDE.md build decisions).
CREATE TABLE owner (
  id         INTEGER PRIMARY KEY CHECK (id = 1),
  google_sub TEXT NOT NULL,
  email      TEXT NOT NULL,
  pinned_at  TEXT NOT NULL
);
CREATE TRIGGER owner_no_update BEFORE UPDATE ON owner
BEGIN SELECT RAISE(ABORT, 'owner is insert-once'); END;
CREATE TRIGGER owner_no_delete BEFORE DELETE ON owner
BEGIN SELECT RAISE(ABORT, 'owner is insert-once'); END;
CREATE TRIGGER owner_no_replace BEFORE INSERT ON owner
WHEN EXISTS (SELECT 1 FROM owner)
BEGIN SELECT RAISE(ABORT, 'owner is insert-once'); END;

CREATE TABLE auth_log (
  id         INTEGER PRIMARY KEY,
  at         TEXT,
  email      TEXT,
  google_sub TEXT,
  result     TEXT
);
CREATE TRIGGER auth_log_no_update BEFORE UPDATE ON auth_log
BEGIN SELECT RAISE(ABORT, 'auth_log is append-only'); END;
CREATE TRIGGER auth_log_no_delete BEFORE DELETE ON auth_log
BEGIN SELECT RAISE(ABORT, 'auth_log is append-only'); END;
CREATE TRIGGER auth_log_no_replace BEFORE INSERT ON auth_log
WHEN EXISTS (SELECT 1 FROM auth_log WHERE id = NEW.id)
BEGIN SELECT RAISE(ABORT, 'auth_log is append-only'); END;

-- Current state (TR-9): one row per original create, showing its latest
-- version (latest amend, else the create). Voided entries are INCLUDED with
-- status = 'voided' (api-contract: lists/export show them struck through);
-- every total/count must filter status <> 'voided'.
CREATE VIEW v_current AS
WITH heads AS (
  SELECT c.id AS entry_id,
         COALESCE((SELECT MAX(a.id) FROM entries a WHERE a.ref_id = c.id AND a.kind = 'amend'), c.id) AS version_id,
         (SELECT COUNT(*) FROM entries a WHERE a.ref_id = c.id AND a.kind = 'amend') AS amend_count,
         (SELECT MIN(v.id) FROM entries v WHERE v.ref_id = c.id AND v.kind = 'void') AS void_id
  FROM entries c
  WHERE c.kind = 'create'
)
SELECT h.entry_id                    AS id,
       h.version_id                  AS version_id,
       e.direction                   AS direction,
       e.occurred_on                 AS occurred_on,
       e.amount_sen                  AS amount_sen,
       e.currency                    AS currency,
       e.category_id                 AS category_id,
       COALESCE(cat.name, '')        AS category_name,
       e.payee                       AS payee,
       e.method                      AS method,
       e.note                        AS note,
       e.receipt_sha256              AS receipt_sha256,
       CASE WHEN h.void_id IS NOT NULL THEN 'voided'
            WHEN h.amend_count > 0     THEN 'amended'
            ELSE 'active' END        AS status,
       h.amend_count                 AS amend_count,
       vd.void_reason                AS void_reason,
       c.created_at                  AS created_at,
       last.created_at               AS updated_at,
       last.hash                     AS hash
FROM heads h
JOIN entries c    ON c.id = h.entry_id
JOIN entries e    ON e.id = h.version_id
JOIN entries last ON last.id = MAX(h.version_id, COALESCE(h.void_id, 0))
LEFT JOIN entries vd     ON vd.id = h.void_id
LEFT JOIN categories cat ON cat.id = e.category_id;
