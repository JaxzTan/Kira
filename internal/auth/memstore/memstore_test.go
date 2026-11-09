package memstore

import (
	"testing"
	"time"

	"github.com/JaxzTan/kira/internal/auth"
)

var _ auth.Store = (*Store)(nil)

func TestStore(t *testing.T) {
	ctx := t.Context()
	now := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	s := New()
	cases := []struct {
		name string
		fn   func() error
	}{
		{"create", func() error { return s.CreateSession(ctx, "h1", "e", "s", "ua", now, now.Add(time.Hour)) }},
		{"create expired", func() error { return s.CreateSession(ctx, "h2", "e", "s", "ua", now, now) }},
		{"pin", func() error { return s.PinOwner(ctx, "sub", "e", now) }},
		{"log", func() error { return s.LogAuth(ctx, now, "e", "sub", "ok") }},
		{"gc", func() error { return s.DeleteExpiredSessions(ctx, now) }},
	}
	for _, c := range cases {
		if err := c.fn(); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
	}
	if err := s.CreateSession(ctx, "h1", "e", "s", "", now, now); err == nil {
		t.Fatal("duplicate session accepted")
	}
	if err := s.PinOwner(ctx, "other", "e", now); err == nil {
		t.Fatal("second pin accepted")
	}
	if sub, ok, _ := s.OwnerSub(ctx); !ok || sub != "sub" {
		t.Fatalf("owner = %q %v", sub, ok)
	}
	if _, _, _, ok, _ := s.GetSession(ctx, "h2"); ok {
		t.Fatal("expired session not collected")
	}
	if e, sub, _, ok, _ := s.GetSession(ctx, "h1"); !ok || e != "e" || sub != "s" {
		t.Fatal("session lost")
	}
	_ = s.DeleteSession(ctx, "h1")
	if len(s.Sessions()) != 0 || len(s.AuthLog()) != 1 {
		t.Fatal("unexpected state")
	}
}
