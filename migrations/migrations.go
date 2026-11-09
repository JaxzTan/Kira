// Package migrations embeds Kira's SQL schema migrations.
//
// Files are named NNNN_description.sql and applied in lexical order by
// internal/store, each in its own transaction, exactly once (tracked in
// schema_migrations). Never edit a migration that has shipped; add a new one.
package migrations

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

//go:embed *.sql
var files embed.FS

// Migration is one embedded schema step.
type Migration struct {
	Version int
	Name    string
	SQL     string
}

// All returns every embedded migration ordered by version.
func All() ([]Migration, error) {
	return parse(files)
}

func parse(fsys fs.FS) ([]Migration, error) {
	names, err := fs.Glob(fsys, "*.sql")
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	out := make([]Migration, 0, len(names))
	seen := map[int]string{}
	for _, n := range names {
		prefix, _, ok := strings.Cut(n, "_")
		if !ok {
			return nil, fmt.Errorf("migrations: %q: want NNNN_name.sql", n)
		}
		v, err := strconv.Atoi(prefix)
		if err != nil || v <= 0 {
			return nil, fmt.Errorf("migrations: %q: bad version prefix", n)
		}
		if prev, dup := seen[v]; dup {
			return nil, fmt.Errorf("migrations: duplicate version %d (%s, %s)", v, prev, n)
		}
		seen[v] = n
		b, err := fs.ReadFile(fsys, n)
		if err != nil {
			return nil, err
		}
		out = append(out, Migration{Version: v, Name: n, SQL: string(b)})
	}
	for i, m := range out {
		if m.Version != i+1 {
			return nil, fmt.Errorf("migrations: version gap at %s (want %d)", m.Name, i+1)
		}
	}
	return out, nil
}
