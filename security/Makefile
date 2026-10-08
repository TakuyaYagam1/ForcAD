SHELL := /usr/bin/env bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := help

ROOT := $(abspath ..)
REPORTS ?= $(CURDIR)/reports
CACHE ?= $(CURDIR)/.cache
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

.PHONY: help check filesystem secrets semgrep rules test image

help: ## Show available checks.
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z_-]+:.*## / {printf "%-16s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

$(REPORTS) $(CACHE):
	umask 077; mkdir -p "$@"

check: ## Run source checks; findings and scanner errors both fail the command.
	@status=0; for target in filesystem secrets semgrep; do \
		$(MAKE) "$$target" || status=1; \
	done; exit "$$status"

filesystem: | $(REPORTS) $(CACHE) ## Scan dependencies and Dockerfiles for HIGH/CRITICAL findings.
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

secrets: | $(REPORTS) $(CACHE) ## Scan Git files and all reachable history; redact secret values.
	$(PYTHON) scan_inputs.py --root "$(ROOT)" --scanner "$(GITLEAKS)" \
		--config "$(CURDIR)/gitleaks/gitleaks.toml" --reports "$(REPORTS)" --cache "$(CACHE)"

semgrep: | $(REPORTS) $(CACHE) ## Check Python and React source with local rules.
	@umask 077; rm -f "$(REPORTS)/semgrep.json"; \
	$(SEMGREP) --version > "$(REPORTS)/semgrep-version.txt"; \
	cd "$(ROOT)"; \
	$(SEMGREP) scan --strict --error --disable-nosem --metrics off \
		--config security/semgrep/rules.yml \
		--json-output "$(REPORTS)/semgrep.json" backend cli front/src

rules: | $(CACHE) ## Validate local Semgrep rules against positive and negative fixtures.
	cd "$(ROOT)" && $(SEMGREP) --test --metrics off \
		--config security/semgrep security/semgrep/tests

test: rules | $(CACHE) ## Check rule fixtures and secret-scan input/error handling.
	PYTHONDONTWRITEBYTECODE=1 PYTHONPATH="$(CURDIR)" TMPDIR="$(CACHE)" \
		$(PYTHON) -m unittest discover -s tests -v

image: | $(REPORTS) $(CACHE) ## Scan an existing local Docker image: make image IMAGE=name:tag.
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
