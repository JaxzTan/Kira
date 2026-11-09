//go:build e2e

package config

// e2eBuild reports whether this binary was built with -tags e2e. E2E builds
// never read .env (tests pass every variable explicitly) and are the only
// builds that honour OIDC_ISSUER_URL.
const e2eBuild = true
