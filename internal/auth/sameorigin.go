package auth

import (
	"net/http"
	"strings"
)

// SameOrigin reports whether a state-changing request is same-origin
// (docs/api-contract.md "Phase 4 hardening", CSRF):
//
//   - Sec-Fetch-Site present: accept only "same-origin".
//   - Sec-Fetch-Site absent: accept only if Origin equals publicOrigin
//     (scheme://host[:port], e.g. derived from PUBLIC_URL). An empty
//     publicOrigin disables this fallback.
//
// Everything else (cross-site, same-site, "none", Origin "null", no headers
// at all) is rejected. kira-api uses this for non-GET /api/* requests;
// POST /auth/logout uses it internally.
func SameOrigin(r *http.Request, publicOrigin string) bool {
	if sfs := r.Header.Values("Sec-Fetch-Site"); len(sfs) > 0 {
		return len(sfs) == 1 && strings.EqualFold(strings.TrimSpace(sfs[0]), "same-origin")
	}
	want := strings.TrimRight(strings.TrimSpace(publicOrigin), "/")
	if want == "" {
		return false
	}
	origins := r.Header.Values("Origin")
	if len(origins) != 1 {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(origins[0]), want)
}
