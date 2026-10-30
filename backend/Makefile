GO ?= go
NODE ?= node
PYTHON ?= python3
PNPM ?= pnpm
GOLANGCI_LINT = $(GO) tool golangci-lint
SQLFLUFF_ENV := .cache/sqlfluff
SQLFLUFF = $(CURDIR)/$(SQLFLUFF_ENV)/bin/python -m sqlfluff
LINT_DEPS := $(SQLFLUFF_ENV)/.lint-deps

.PHONY: sync sync-codegen build test test-checker test-load test-contracts test-integration integration race vet lint lint-go lint-sql fmt fmt-check install-tools generate oapi-generate gen-openapi gen-sqlc gen-wire

sync: sync-codegen install-tools

sync-codegen:
	PYTHON="$(PYTHON)" PNPM="$(PNPM)" bash ../api/scripts/install-tools.sh

build:
	$(GO) build -trimpath -o bin/forcad ./cmd/forcad

test:
	$(GO) test ./cmd/... ./config/... ./db/... ./internal/... ./load_test

test-checker:
	PYTHONDONTWRITEBYTECODE=1 $(GO) test -race -count=1 ./integration_test/checker -run '^TestExample'

test-load:
	bash ./load_test/scripts/run.sh

test-contracts:
	cd ../api && $(PNPM) test

test-integration integration:
	./integration_test/scripts/run_tests.sh

race:
	$(GO) test -race ./cmd/... ./config/... ./db/... ./internal/... ./load_test

vet:
	$(GO) vet ./...

install-tools: $(LINT_DEPS)
	$(GO) mod download
	$(GOLANGCI_LINT) version

$(LINT_DEPS): requirements.txt
	$(PYTHON) -m venv $(SQLFLUFF_ENV)
	$(SQLFLUFF_ENV)/bin/python -m pip install --only-binary=:all: -r requirements.txt
	@touch "$@"

lint: lint-go lint-sql

lint-go:
	$(GOLANGCI_LINT) config verify
	$(GOLANGCI_LINT) run ./...

lint-sql: $(LINT_DEPS)
	$(SQLFLUFF) lint --config .sqlfluff db/migrations db/queries

fmt:
	$(GOLANGCI_LINT) fmt ./...

fmt-check:
	@diff=$$($(GOLANGCI_LINT) fmt --diff ./...) || exit $$?; \
	if [ -n "$$diff" ]; then printf '%s\n' "$$diff"; exit 1; fi

generate: gen-openapi gen-sqlc gen-wire

oapi-generate gen-openapi:
	$(NODE) ../api/scripts/generate.mjs backend

gen-sqlc:
	$(GO) tool sqlc generate -f codegen/sqlc.yml

gen-wire:
	$(GO) tool wire ./internal/bootstrap
