PNPM ?= pnpm
PYTHON ?= python3

.PHONY: sync sync-codegen build dev generate oapi-generate lint typecheck test e2e

sync: sync-codegen
	$(PNPM) install --frozen-lockfile

sync-codegen:
	PYTHON="$(PYTHON)" PNPM="$(PNPM)" bash ../api/scripts/install-tools.sh

build:
	$(PNPM) build

dev:
	$(PNPM) dev

generate oapi-generate:
	$(PNPM) generate:api

lint:
	$(PNPM) lint
	$(MAKE) typecheck

typecheck:
	$(PNPM) exec tsc -b --pretty false
	$(PNPM) typecheck:e2e

test:
	$(PNPM) test

e2e:
	$(MAKE) -C ../deployment e2e
