# Phase 0 skeleton. kira-ops owns and extends this (dev, build, e2e, verify, backup-test).
SHELL := /bin/bash
.PHONY: ensure-dist fmt vet test web-check check check-env

ensure-dist:
	@mkdir -p web/dist && { [ -f web/dist/index.html ] || touch web/dist/.keep; }

fmt:
	@out=$$(gofmt -l $$(git ls-files -co --exclude-standard '*.go')); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

vet: ensure-dist
	go vet ./...

test: ensure-dist
	go test ./...

web-check:
	cd web && npm run build

check: fmt vet test web-check

check-env:
	@bash scripts/check-env.sh
