.PHONY: lint-backend
lint-backend:
	ruff check backend
	flake8 --config .flake8

PYTHON ?= python

.PHONY: lint-frontend
lint-frontend:
	cd front && pnpm run lint && pnpm exec tsc -b --pretty false

.PHONY: lint-sql
lint-sql:
	sqlfluff lint backend/scripts

.PHONY: lint
lint: lint-backend lint-frontend lint-sql

.PHONY: clean
clean:
	./control.py clean

.PHONY: test test-integration
test:
	$(PYTHON) scripts/run_unit_tests.py

test-integration:
	@if [ "$${FORCAD_ALLOW_DESTRUCTIVE_TESTS:-}" != "1" ]; then \
		echo "Refusing integration tests: scripts/run_tests.sh resets the local ForcAD database." >&2; \
		echo "Run this target only in a disposable checkout with FORCAD_ALLOW_DESTRUCTIVE_TESTS=1." >&2; \
		exit 2; \
	fi
	./scripts/run_tests.sh

.PHONY: build-base
build-base:
	FORCAD_BASE_IMAGE=forcad_base:local ./scripts/release_base.sh

.PHONY: release-base
release-base:
	@if [ -z "$${FORCAD_BASE_IMAGE:-}" ]; then \
		echo "Set FORCAD_BASE_IMAGE=ghcr.io/<owner>/forcad_base:<version> before publishing." >&2; \
		exit 2; \
	fi
	./scripts/release_base.sh --push

.PHONY: start
start:
	./control.py setup
	./control.py start
	./control.py rd logs -f initializer
