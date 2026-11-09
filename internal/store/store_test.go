package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "data", "kira.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestOpenRejectsBadPaths(t *testing.T) {
	for _, p := range []string{"", ":memory:", "file:x.db", "a?b.db", "a#b.db"} {
		t.Run(p, func(t *testing.T) {
			if s, err := Open(context.Background(), p); err == nil {
				s.Close()
				t.Fatalf("Open(%q) succeeded", p)
			}
		})
	}
}

func TestOpenPragmasAndPerms(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "with space", "data")
	path := filepath.Join(dir, "kira.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	cases := []struct {
		pragma string
		want   string
	}{
		{"journal_mode", "wal"},
		{"synchronous", "2"},
		{"foreign_keys", "1"},
		{"busy_timeout", "5000"},
	}
	// Check on several pooled connections, not just the first.
	for i := 0; i < 3; i++ {
		conn, err := s.DB().Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		for _, tc := range cases {
			var got string
			if err := conn.QueryRowContext(ctx, "PRAGMA "+tc.pragma).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if !strings.EqualFold(got, tc.want) {
				t.Errorf("conn %d PRAGMA %s = %s, want %s", i, tc.pragma, got, tc.want)
			}
		}
	}
	for _, c := range []struct {
		p    string
		mode os.FileMode
	}{{dir, 0o700}, {path, 0o600}} {
		fi, err := os.Stat(c.p)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != c.mode {
			t.Errorf("%s mode = %o, want %o", c.p, fi.Mode().Perm(), c.mode)
		}
	}
}

func TestMigrateIdempotentAndSeedOnce(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "kira.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateCategory(ctx, "Courses", nil); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	v, err := s.SchemaVersion(ctx)
	if err != nil || v != 3 {
		t.Fatalf("schema version = %d, %v", v, err)
	}
	cats, err := s.ListCategories(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(cats) != 11 {
		t.Fatalf("got %d categories after reopen, want 11 (10 seeds + 1)", len(cats))
	}
}

func TestSeedCategories(t *testing.T) {
	s := openTest(t)
	cats, err := s.ListCategories(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct{ name, color string }{
		{"Cloud & hosting", "#0F766E"},
		{"Software & subscriptions", "#2F6FB5"},
		{"Hardware", "#C2701E"},
		{"Internet & phone", "#7A5BB0"},
		{"Travel & transport", "#9AA09B"},
		{"Meals & entertainment", "#0F766E"},
		{"Professional fees", "#2F6FB5"},
		{"Marketing", "#C2701E"},
		{"Office", "#7A5BB0"},
		{"Other", "#9AA09B"},
	}
	if len(cats) != len(want) {
		t.Fatalf("got %d seeds", len(cats))
	}
	for i, w := range want {
		c := cats[i]
		if c.ID != int64(i+1) || c.Name != w.name || c.Color == nil || *c.Color != w.color || c.ArchivedAt != nil {
			t.Errorf("seed %d = %+v, want %s %s", i, c, w.name, w.color)
		}
	}
}

func TestAppendOnlyTriggers(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	db := s.DB()
	mustExec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustExec(`INSERT INTO entries (id, kind, occurred_on, amount_sen, category_id, payee, method, created_at, prev_hash, hash)
	          VALUES (1, 'create', '2026-11-09', 100, 1, 'x', 'card', '2026-11-09T00:00:00Z', ?, 'h1')`, strings.Repeat("0", 64))
	if err := s.LogAuth(ctx, time.Now(), "a@b.c", "sub", "ok"); err != nil {
		t.Fatal(err)
	}
	if err := s.PinOwner(ctx, "sub", "a@b.c", time.Now()); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, sql, wantMsg string
	}{
		{"entries update", `UPDATE entries SET amount_sen = 1 WHERE id = 1`, "ledger is append-only"},
		{"entries delete", `DELETE FROM entries WHERE id = 1`, "ledger is append-only"},
		{"entries replace same id", `INSERT OR REPLACE INTO entries (id, kind, occurred_on, amount_sen, created_at, prev_hash, hash) VALUES (1,'create','2026-01-01',5,'t','p','hX')`, "ledger is append-only"},
		{"entries replace same hash", `REPLACE INTO entries (id, kind, occurred_on, amount_sen, created_at, prev_hash, hash) VALUES (7,'create','2026-01-01',5,'t','p','h1')`, "ledger is append-only"},
		{"entries upsert", `INSERT INTO entries (id, kind, occurred_on, amount_sen, created_at, prev_hash, hash) VALUES (2,'create','2026-01-01',5,'t','p','h1') ON CONFLICT(hash) DO UPDATE SET amount_sen = 5`, "ledger is append-only"},
		{"auth_log replace", `INSERT OR REPLACE INTO auth_log (id, result) VALUES (1, 'x')`, "auth_log is append-only"},
		{"owner replace", `INSERT OR REPLACE INTO owner (id, google_sub, email, pinned_at) VALUES (1, 'evil', 'e', 't')`, "owner is insert-once"},
		{"auth_log update", `UPDATE auth_log SET result = 'x'`, "auth_log is append-only"},
		{"auth_log delete", `DELETE FROM auth_log`, "auth_log is append-only"},
		{"owner update", `UPDATE owner SET google_sub = 'evil'`, "owner is insert-once"},
		{"owner delete", `DELETE FROM owner`, "owner is insert-once"},
		{"owner second row", `INSERT INTO owner (id, google_sub, email, pinned_at) VALUES (2, 's', 'e', 't')`, "insert-once"},
		{"void with ref null", `INSERT INTO entries (id, kind, created_at, prev_hash, hash) VALUES (2, 'void', 't', 'p', 'h2')`, "CHECK"},
		{"amount zero", `INSERT INTO entries (id, kind, occurred_on, amount_sen, created_at, prev_hash, hash) VALUES (2,'create','2026-01-01',0,'t','p','h2')`, "CHECK"},
		{"bad category fk", `INSERT INTO entries (id, kind, occurred_on, amount_sen, category_id, created_at, prev_hash, hash) VALUES (2,'create','2026-01-01',5,999,'t','p','h2')`, "FOREIGN KEY"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := db.ExecContext(ctx, tc.sql)
			if err == nil {
				t.Fatal("statement succeeded, want abort")
			}
			if tc.wantMsg != "" && !strings.Contains(err.Error(), tc.wantMsg) {
				t.Fatalf("err = %v, want %q", err, tc.wantMsg)
			}
		})
	}
	var amt int64
	if err := db.QueryRowContext(ctx, `SELECT amount_sen FROM entries WHERE id = 1`).Scan(&amt); err != nil || amt != 100 {
		t.Fatalf("row changed: amt=%d err=%v", amt, err)
	}
	if !IsAppendOnlyError(errors.New("ledger is append-only")) || IsAppendOnlyError(nil) {
		t.Fatal("IsAppendOnlyError")
	}
}

func TestSessions(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 9, 3, 0, 0, 0, time.UTC)
	if err := s.CreateSession(ctx, "tokA", "a@b.c", "subA", "UA", now, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession(ctx, "tokB", "a@b.c", "subA", "UA", now, now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession(ctx, "tokA", "a@b.c", "subA", "UA", now, now); err == nil {
		t.Fatal("duplicate token accepted")
	}
	cases := []struct {
		tok       string
		wantFound bool
	}{{"tokA", true}, {"tokB", true}, {"nope", false}}
	for _, tc := range cases {
		email, sub, exp, found, err := s.GetSession(ctx, tc.tok)
		if err != nil || found != tc.wantFound {
			t.Fatalf("%s: found=%v err=%v", tc.tok, found, err)
		}
		if found && (email != "a@b.c" || sub != "subA" || exp.IsZero()) {
			t.Fatalf("%s: %s %s %v", tc.tok, email, sub, exp)
		}
	}
	_, _, exp, _, _ := s.GetSession(ctx, "tokA")
	if !exp.Equal(now.Add(time.Hour)) {
		t.Fatalf("expiresAt = %v", exp)
	}
	if err := s.DeleteExpiredSessions(ctx, now); err != nil {
		t.Fatal(err)
	}
	if _, _, _, found, _ := s.GetSession(ctx, "tokB"); found {
		t.Fatal("expired session not deleted")
	}
	if _, _, _, found, _ := s.GetSession(ctx, "tokA"); !found {
		t.Fatal("live session deleted")
	}
	if err := s.DeleteSession(ctx, "tokA"); err != nil {
		t.Fatal(err)
	}
	if _, _, _, found, _ := s.GetSession(ctx, "tokA"); found {
		t.Fatal("session not deleted")
	}
	if err := s.DeleteSession(ctx, "tokA"); err != nil {
		t.Fatal("deleting missing session errored")
	}
}

func TestPinOwner(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	if _, found, err := s.OwnerSub(ctx); found || err != nil {
		t.Fatalf("fresh db has owner: %v %v", found, err)
	}
	steps := []struct {
		sub     string
		wantErr error
	}{
		{"sub-1", nil},
		{"sub-1", nil},
		{"sub-2", ErrOwnerMismatch},
		{"", ErrInvalid},
	}
	for i, st := range steps {
		err := s.PinOwner(ctx, st.sub, "a@b.c", time.Now())
		if !errors.Is(err, st.wantErr) {
			t.Fatalf("step %d: err = %v, want %v", i, err, st.wantErr)
		}
	}
	sub, found, err := s.OwnerSub(ctx)
	if err != nil || !found || sub != "sub-1" {
		t.Fatalf("owner = %q %v %v", sub, found, err)
	}
}

func TestAuthLog(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	for _, r := range []string{"ok", "reject_email", "reject_sub"} {
		if err := s.LogAuth(ctx, time.Now(), "a@b.c", "s", r); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.RecentAuthLog(ctx, 2)
	if err != nil || len(got) != 2 || got[0].Result != "reject_sub" {
		t.Fatalf("got %+v %v", got, err)
	}
}

func TestCategories(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	str := func(v string) *string { return &v }
	createCases := []struct {
		name    string
		color   *string
		wantErr error
	}{
		{"Courses", str("#2F6FB5"), nil},
		{"  Books  ", nil, nil},
		{"courses", nil, ErrDuplicateName},
		{"CLOUD & HOSTING", nil, ErrDuplicateName},
		{"", nil, ErrInvalid},
		{"   ", nil, ErrInvalid},
		{strings.Repeat("x", 61), nil, ErrInvalid},
		{strings.Repeat("é", 60), nil, nil},
		{"Bad color", str("red"), ErrInvalid},
		{"Ünïcode", nil, nil},
		{"üNÏCODE", nil, ErrDuplicateName},
	}
	for _, tc := range createCases {
		t.Run("create "+tc.name, func(t *testing.T) {
			c, err := s.CreateCategory(ctx, tc.name, tc.color)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if err == nil && (c.Name != strings.TrimSpace(tc.name) || c.Color == nil) {
				t.Fatalf("got %+v", c)
			}
		})
	}
	books, err := s.RenameCategory(ctx, 12, "Books & media")
	if err != nil || books.Name != "Books & media" {
		t.Fatalf("rename: %+v %v", books, err)
	}
	renameCases := []struct {
		id      int64
		name    string
		wantErr error
	}{
		{12, "books & MEDIA", nil}, // same row, case change ok
		{12, "Hardware", ErrDuplicateName},
		{999, "Nope", ErrNotFound},
		{12, "", ErrInvalid},
	}
	for _, tc := range renameCases {
		if _, err := s.RenameCategory(ctx, tc.id, tc.name); !errors.Is(err, tc.wantErr) {
			t.Fatalf("rename %d %q: err = %v, want %v", tc.id, tc.name, err, tc.wantErr)
		}
	}
	at := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	c, err := s.ArchiveCategory(ctx, 3, at)
	if err != nil || c.ArchivedAt == nil || *c.ArchivedAt != "2026-11-09T00:00:00Z" {
		t.Fatalf("archive: %+v %v", c, err)
	}
	if c, _ := s.ArchiveCategory(ctx, 3, at.Add(time.Hour)); *c.ArchivedAt != "2026-11-09T00:00:00Z" {
		t.Fatal("re-archive changed archived_at")
	}
	active, _ := s.ListCategories(ctx, false)
	all, _ := s.ListCategories(ctx, true)
	if len(all)-len(active) != 1 {
		t.Fatalf("active %d all %d", len(active), len(all))
	}
	if c, err := s.UnarchiveCategory(ctx, 3); err != nil || c.ArchivedAt != nil {
		t.Fatalf("unarchive: %+v %v", c, err)
	}
	if _, err := s.ArchiveCategory(ctx, 999, at); !errors.Is(err, ErrNotFound) {
		t.Fatalf("archive missing: %v", err)
	}
	if _, err := s.GetCategory(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get missing: %v", err)
	}
}
