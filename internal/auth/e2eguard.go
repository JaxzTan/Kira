//go:build !e2e

package auth

// E2EGuard is called by the server at startup. In normal builds there is no
// test login, so it always returns nil.
func E2EGuard() error { return nil }
