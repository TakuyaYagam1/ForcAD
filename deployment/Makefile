SHELL := /usr/bin/env bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := help

ROOT := $(abspath ..)
REPORTS ?= $(ROOT)/security/reports
CACHE ?= $(ROOT)/security/.cache
TRIVY ?= trivy
GITLEAKS ?= gitleaks
SEMGREP ?= semgrep
PYTHON ?= python3
DOCKER ?= docker

export SEMGREP_SEND_METRICS := off
export SEMGREP_ENABLE_VERSION_CHECK := 0
export SEMGREP_SETTINGS_FILE := $(CACHE)/semgrep-settings.yml
export SEMGREP_LOG_FILE := $(CACHE)/semgrep.log
# Semgrep also runs on hosts where io_uring is unavailable or restricted.
export EIO_BACKEND ?= posix

.PHONY: help test test-integration test-deployment test-load test-monitoring test-network e2e security security-filesystem security-secrets security-semgrep security-go-vuln security-rules security-test security-image

help: ## Show available checks.
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z0-9_-]+:.*## / {printf "%-22s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

test: ## Run infrastructure integration tests, then deployment API checks.
	$(MAKE) test-integration
	$(MAKE) test-deployment

test-integration: ## Test backend workflows against isolated storage containers.
	../backend/integration_test/scripts/run_tests.sh

test-deployment: ## Build and check an isolated production stack through its API.
	../backend/integration_test/scripts/test_deployment.sh

test-load: ## Exercise 20 flag farms, checker rounds and reconnecting spectators.
	../backend/load_test/scripts/run.sh

test-monitoring: ## Verify team metrics, Prometheus discovery and Grafana in an isolated stack.
	../backend/integration_test/scripts/test_deployment.sh --monitoring

test-network: ## Check nftables and its exporter in a disposable network namespace.
	bash ../backend/integration_test/scripts/test_network.sh

e2e: ## Check an isolated production stack through Playwright and its API.
	../backend/integration_test/scripts/test_deployment.sh --e2e

$(REPORTS) $(CACHE):
	umask 077; mkdir -p "$@"

security: ## Run source checks; findings and scanner errors both fail the command.
	@status=0; for target in security-filesystem security-secrets security-semgrep security-go-vuln; do \
		$(MAKE) "$$target" || status=1; \
	done; exit "$$status"

security-filesystem: | $(REPORTS) $(CACHE) ## Scan dependencies and Dockerfiles for HIGH/CRITICAL findings.
	@umask 077; rm -f "$(REPORTS)/trivy-filesystem.json"; \
	cd "$(ROOT)"; status=0; \
	$(TRIVY) fs --no-progress --config security/trivy/filesystem.yml --cache-dir "$(CACHE)/trivy" \
		--output "$(REPORTS)/trivy-filesystem.json" . || status=$$?; \
	if [ -s "$(REPORTS)/trivy-filesystem.json" ]; then \
		$(TRIVY) convert --format table --table-mode summary --scanners vuln,misconfig \
			"$(REPORTS)/trivy-filesystem.json" || status=1; \
	else echo 'Trivy did not create a report.' >&2; status=1; fi; \
	$(TRIVY) --cache-dir "$(CACHE)/trivy" --version > "$(REPORTS)/trivy-version.txt" || status=1; \
	exit "$$status"

security-secrets: | $(REPORTS) $(CACHE) ## Scan Git files and all reachable history; redact secret values.
	$(PYTHON) "$(ROOT)/security/scan_inputs.py" --root "$(ROOT)" --scanner "$(GITLEAKS)" \
		--config "$(ROOT)/security/gitleaks/gitleaks.toml" --reports "$(REPORTS)" --cache "$(CACHE)"

security-semgrep: | $(REPORTS) $(CACHE) ## Check source with local security rules.
	@umask 077; rm -f "$(REPORTS)/semgrep.json"; \
	$(SEMGREP) --version > "$(REPORTS)/semgrep-version.txt"; \
	cd "$(ROOT)"; \
	$(SEMGREP) scan --strict --error --disable-nosem --metrics off \
		--config security/semgrep/rules.yml \
		--json-output "$(REPORTS)/semgrep.json" backend/internal backend/config backend/cmd frontend/src

security-rules: | $(CACHE) ## Validate local Semgrep rules against positive and negative fixtures.
	cd "$(ROOT)" && $(SEMGREP) --test --metrics off \
		--config security/semgrep security/semgrep/tests

security-test: security-rules | $(CACHE) ## Check rule fixtures and secret-scan input/error handling.
	PYTHONDONTWRITEBYTECODE=1 PYTHONPATH="$(ROOT)/security" TMPDIR="$(CACHE)" \
		$(PYTHON) -m unittest discover -s "$(ROOT)/security/tests" -v

security-image: | $(REPORTS) $(CACHE) ## Scan an existing local Docker image: make security-image IMAGE=name:tag.
	@test -n "$(IMAGE)" || { echo 'Set IMAGE to an existing local Docker image.' >&2; exit 2; }
	@umask 077; rm -f "$(REPORTS)/trivy-image.json" "$(REPORTS)/image-id.txt"; \
	image_id=$$($(DOCKER) image inspect --format '{{.Id}}' "$(IMAGE)"); \
	printf '%s\n' "$$image_id" > "$(REPORTS)/image-id.txt"; \
	cd "$(ROOT)"; status=0; \
	$(TRIVY) image --no-progress --image-src docker --config security/trivy/image.yml \
		--cache-dir "$(CACHE)/trivy" --output "$(REPORTS)/trivy-image.json" "$$image_id" || status=$$?; \
	if [ -s "$(REPORTS)/trivy-image.json" ]; then \
		$(TRIVY) convert --format table --table-mode summary --scanners vuln,misconfig \
			"$(REPORTS)/trivy-image.json" || status=1; \
	else echo 'Trivy did not create an image report.' >&2; status=1; fi; \
	$(TRIVY) --cache-dir "$(CACHE)/trivy" --version > "$(REPORTS)/trivy-version.txt" || status=1; \
	exit "$$status"

security-go-vuln: ## Check reachable vulnerabilities in Go dependencies.
	cd "$(ROOT)/backend" && go tool govulncheck ./...
