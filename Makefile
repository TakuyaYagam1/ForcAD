UV ?= uv
RUN = $(UV) run --project backend --locked --all-groups
FORCAD = $(UV) run --project backend --locked --group cli forcad

.PHONY: sync setup up start build dev lint lint-backend lint-frontend lint-sql test test-integration clean

sync:
	$(UV) sync --project backend --locked --all-groups

setup:
	$(FORCAD) setup

up start:
	$(FORCAD) start

build:
	$(FORCAD) build

dev:
	$(FORCAD) rd -f deployment/docker/compose.dev.yml up -d --build

lint-backend:
	$(RUN) ruff check backend/src
	$(RUN) flake8 --config .flake8 backend/src scripts

lint-frontend:
	cd frontend && pnpm run lint && pnpm exec tsc -b --pretty false

lint-sql:
	$(RUN) python scripts/lint_sql.py --uv "$(UV)" lint backend/src/forcad/scripts

lint: lint-backend lint-frontend lint-sql

test:
	$(RUN) python scripts/run_unit_tests.py

test-integration:
	@if [ "$${FORCAD_ALLOW_DESTRUCTIVE_TESTS:-}" != "1" ]; then \
		echo "Run integration tests only in a disposable checkout with FORCAD_ALLOW_DESTRUCTIVE_TESTS=1." >&2; \
		exit 2; \
	fi
	$(RUN) bash scripts/run_tests.sh

clean:
	$(FORCAD) clean
