# Tax P&L Engine — common commands. Run from Git Bash / WSL on Windows.
SHELL := /bin/sh
-include .env
export

GO        ?= go
GOBIN     ?= $(subst \,/,$(shell $(GO) env GOPATH))/bin
BACKEND   := backend
FRONTEND  := frontend
DATABASE_URL ?= postgres://taxpl:taxpl_dev_password@localhost:5432/taxpl?sslmode=disable

.PHONY: app app-down up down reset-db migrate migrate-down sqlc api worker web test test-golden test-integration lint seed bench check hooks tools fmt

## Whole app in Docker (Postgres, Redis, migrations, api, worker, web on :8081)
app:           ## build and start everything
	docker compose --profile app up -d --build
	@echo "open http://localhost:$${WEB_PORT:-8081}"

app-down:      ## stop the app containers (keeps data)
	docker compose --profile app down

## Infrastructure
up:            ## start Postgres + Redis
	docker compose up -d postgres redis
	@echo "waiting for postgres..." && until docker exec taxpl-postgres pg_isready -U taxpl -d taxpl >/dev/null 2>&1; do sleep 1; done
	@echo "ready"

down:          ## stop containers (keep data)
	docker compose down

reset-db:      ## destroy and recreate the database volume
	docker compose down -v
	$(MAKE) up
	$(MAKE) migrate

## Database
migrate:       ## run goose migrations
	cd $(BACKEND) && $(GOBIN)/goose -dir db/migrations postgres "$(DATABASE_URL)" up

migrate-down:  ## roll back one migration
	cd $(BACKEND) && $(GOBIN)/goose -dir db/migrations postgres "$(DATABASE_URL)" down

sqlc:          ## regenerate sqlc code
	cd $(BACKEND) && $(GOBIN)/sqlc generate

## Run
api:           ## run the HTTP API
	cd $(BACKEND) && $(GO) run ./cmd/api

worker:        ## run the River worker
	cd $(BACKEND) && $(GO) run ./cmd/worker

web:           ## run the Vite dev server
	cd $(FRONTEND) && npm run dev

seed:          ## generate a synthetic Zerodha tradebook
	cd $(BACKEND) && $(GO) run ./cmd/seed -out ../.data/seed -trades 500

## Quality
fmt:
	cd $(BACKEND) && gofmt -l -w .

test:          ## unit + golden + property tests (no Docker needed)
	cd $(BACKEND) && $(GO) test ./... -count=1

test-golden:   ## golden-file tests only
	cd $(BACKEND) && $(GO) test ./internal/... -run 'Golden' -count=1 -v

test-integration: ## tests that need Docker (testcontainers)
	cd $(BACKEND) && TAXPL_INTEGRATION=1 $(GO) test ./... -run 'Integration' -count=1

lint:          ## golangci-lint + no-float guard
	cd $(BACKEND) && $(GO) vet ./...
	@test -x $(GOBIN)/golangci-lint || (echo "golangci-lint not installed: run make tools" && exit 1)
	cd $(BACKEND) && $(GOBIN)/golangci-lint run --timeout 20m ./...

bench:         ## benchmarks (results in docs/benchmarks.md)
	cd $(BACKEND) && $(GO) test ./internal/matcher/... ./internal/pipeline/... -run '^$$' -bench . -benchmem

check: lint test ## everything a clean clone must pass

## Setup
hooks:         ## install the commit-msg hook that strips AI attribution
	chmod +x .githooks/commit-msg
	git config core.hooksPath .githooks

tools:         ## install Go CLI tools
	$(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
	$(GO) install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
	$(GO) install github.com/pressly/goose/v3/cmd/goose@latest
