// Package web exposes the built frontend (web/dist) for embedding into the Go binary.
// `make build` runs the Vite build first; `make ensure-dist` creates a placeholder so
// `go vet`/`go test` work before the first frontend build.
package web

import "embed"

// Dist holds web/dist. Serve it with fs.Sub(Dist, "dist").
//
//go:embed all:dist
var Dist embed.FS
