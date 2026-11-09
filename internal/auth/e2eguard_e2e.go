//go:build e2e

package auth

import (
	"errors"
	"os"
)

// E2EEnv must be "1" for an e2e build to start or serve /auth/test-login.
const E2EEnv = "KIRA_E2E"

// E2EGuard is called by the server at startup. In -tags e2e builds it
// refuses to run unless KIRA_E2E=1, so an e2e binary started by mistake
// against the real .env and data/ exits instead of exposing test-login.
func E2EGuard() error {
	if !e2eEnabled() {
		return errors.New("auth: this is an e2e test build (/auth/test-login); refusing to start unless " + E2EEnv + "=1")
	}
	return nil
}

func e2eEnabled() bool { return os.Getenv(E2EEnv) == "1" }
