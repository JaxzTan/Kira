package ledger

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/JaxzTan/kira/internal/store"
)

var testKey = bytes.Repeat([]byte{0x42}, 32)

var testNow = time.Date(2026, 11, 9, 3, 12, 44, 0, time.UTC)

type fakeReceipts map[string]bool

func (f fakeReceipts) Exists(sha string) bool { return f[sha] }

func newTestService(t *testing.T, mods ...func(*Config)) (*Service, *store.Store) {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "kira.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	cfg := Config{Store: st, Key: testKey, Now: func() time.Time { return testNow }, Location: time.UTC}
	for _, m := range mods {
		m(&cfg)
	}
	svc, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return svc, st
}

func sp(s string) *string { return &s }

func input(date string, sen int64, payee string) Input {
	return Input{OccurredOn: date, AmountSen: sen, CategoryID: 1, Payee: payee, Method: MethodCard}
}

func mustCreate(t *testing.T, svc *Service, in Input) AppendResult {
	t.Helper()
	r, err := svc.Create(context.Background(), in)
	if err != nil {
		t.Fatalf("create %+v: %v", in, err)
	}
	return r
}
