-- FR-9 Income. Categories gain an immutable direction; existing rows (and
-- every row created before this migration) are expense via the DEFAULT, so
-- this is safe on a database that already holds entries: entries.direction
-- already existed (and is hashed), no ledger row is touched.
ALTER TABLE categories ADD COLUMN direction TEXT NOT NULL DEFAULT 'expense'
  CHECK (direction IN ('expense','income'));

-- Direction is set at creation and never changes (an income category must
-- not silently turn historic income into expenses).
CREATE TRIGGER categories_direction_immutable BEFORE UPDATE OF direction ON categories
WHEN NEW.direction IS NOT OLD.direction
BEGIN SELECT RAISE(ABORT, 'category direction is immutable'); END;

-- Seed income categories (ids follow the existing max id). A name the owner
-- already uses (NOCASE) is skipped rather than failing the upgrade.
INSERT INTO categories (name, color, direction)
SELECT v.name, v.color, 'income' FROM (
  SELECT 1 AS ord, 'Client revenue' AS name, '#0F766E' AS color
  UNION ALL SELECT 2, 'Product sales',     '#2F6FB5'
  UNION ALL SELECT 3, 'Refunds & rebates', '#C2701E'
  UNION ALL SELECT 4, 'Other income',      '#9AA09B'
) v
WHERE NOT EXISTS (SELECT 1 FROM categories c WHERE c.name = v.name)
ORDER BY v.ord;
