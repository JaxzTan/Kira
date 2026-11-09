# kira-ops owns this file.
SHELL := /bin/bash
.DEFAULT_GOAL := help
.PHONY: help ensure-dist fmt vet test web-check web-test check check-env dev build build-e2e verify e2e backup-test clean

help: ## List targets
	@grep -E '^[a-zA-Z0-9_-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-12s %s\n", $$1, $$2}'

ensure-dist:
	@mkdir -p web/dist && { [ -f web/dist/index.html ] || touch web/dist/.keep; }

fmt: ## Fail if any Go file needs gofmt
	@out=$$(gofmt -l $$(git ls-files -co --exclude-standard '*.go')); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

vet: ensure-dist ## go vet
	go vet ./...

test: ensure-dist ## go test (no -race: needs cgo)
	go test ./...

web-check:
	cd web && npm run build

web-test: ## web unit tests if a test script exists
	@if [ -n "$$(npm --prefix web pkg get scripts.test | tr -d '{}\" \n')" ]; then npm --prefix web test --silent -- --run --passWithNoTests; else echo "web: no test script yet, skipping"; fi

check: fmt vet test web-check web-test ## fmt, vet, test, web typecheck+build+test

check-env: ## Validate .env (never prints values)
	@bash scripts/check-env.sh

dev: ensure-dist ## Go :8080 + Vite :5173 together (Ctrl-C stops both)
	@[ -d web/node_modules ] || npm --prefix web ci
	@mkdir -p data && chmod 700 data
	@trap 'kill 0' INT TERM EXIT; \
	  go run ./cmd/kira serve & \
	  npm --prefix web run dev & \
	  wait

build: ## Build web, then single binary bin/kira
	@[ -d web/node_modules ] || npm --prefix web ci
	npm --prefix web run build
	CGO_ENABLED=0 go build -trimpath -o bin/kira ./cmd/kira

build-e2e: ## Build bin/kira-e2e (with -tags e2e test login)
	@[ -d web/node_modules ] || npm --prefix web ci
	npm --prefix web run build
	CGO_ENABLED=0 go build -trimpath -tags e2e -o bin/kira-e2e ./cmd/kira

verify: build ## Verify the ledger hash chain
	./bin/kira verify

e2e: build-e2e ## Playwright E2E
	@if [ ! -f e2e/package.json ]; then echo "e2e/package.json missing (kira-tester has not created it yet)"; exit 1; fi
	npm --prefix e2e ci
	cd e2e && npx playwright test

backup-test: ## Litestream replicate/restore drill in a temp dir
	@bash scripts/backup-test.sh

clean: ## Remove bin/ and web/dist (data/ is never touched)
	rm -rf bin web/dist
