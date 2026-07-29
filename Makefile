GOPATH := $(shell go env GOPATH)
MUTEST := $(GOPATH)/bin/go-mutesting
GOFUMPT := $(GOPATH)/bin/gofumpt
SYFT := $(shell command -v syft 2>/dev/null)
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
SBOM_FORMAT := cyclonedx-json
SBOM_FILE := sbom.json

# ── Formatting ───────────────────────────────────────────────────────────────

.PHONY: fmt
fmt: $(GOFUMPT)  ## Format code using gofumpt
	$(GOFUMPT) -w .

$(GOFUMPT):
	go install mvdan.cc/gofumpt@latest

# ── Docs / ADRs ───────────────────────────────────────────────────────────────

.PHONY: adr-index docs-lint
adr-index: ## Regenerate docs/adr/README.md from ADR files
	go run ./cmd/adr-lint -write-index -check-index=false

docs-lint: ## Validate ADR template, unique numbers, and index freshness
	go run ./cmd/adr-lint -check-index
	go test ./internal/adr/... -count=1 -cover

# ── Deploy assets (image signing / Kyverno policy) ────────────────────────────

.PHONY: validate-deploy
validate-deploy: ## Static invariants check for release workflow + Kyverno policy
	go test ./internal/deploylint/... -count=1 -v

# ── Mutation testing ──────────────────────────────────────────────────────────

.PHONY: test-coverage
test-coverage:
	go test -coverprofile=coverage.out ./...
	@COVERAGE=$$(go tool cover -func=coverage.out | grep total: | awk '{print $$3}' | tr -d '%'); \
	echo "Total Coverage: $$COVERAGE%"; \
	if [ 1 -eq "$$(echo "$$COVERAGE < 95.0" | bc)" ]; then \
		echo "Coverage is below the 95% threshold! Failing build."; \
		exit 1; \
	fi

.PHONY: mutation-state-machine
mutation-state-machine: $(MUTEST)  ## Run mutation tests on the subscription state machine
	$(MUTEST) ./internal/subscriptions/...

$(MUTEST):
	go install github.com/avito-tech/go-mutesting/cmd/go-mutesting@latest

# ── SBOM ──────────────────────────────────────────────────────────────────────

.PHONY: sbom sbom-install sbom-verify

sbom-install:  ## Install syft if not present
	@if [ -z "$(SYFT)" ]; then \
		echo "Installing syft..."; \
		curl -sSfL https://raw.githubusercontent.com/anchore/syft/main/install.sh | sh -s -- -b /usr/local/bin; \
	fi

sbom: sbom-install  ## Generate a CycloneDX SBOM for the Go module
	$(if $(SYFT),$(SYFT),/usr/local/bin/syft) \
		-o "$(SBOM_FORMAT)=$(SBOM_FILE)" \
		"dir:."
	@echo "SBOM written to $(SBOM_FILE)"

# ── Gitleaks / Secret Scanning ───────────────────────────────────────────────

.PHONY: gitleaks-scan gitleaks-scan-staged gitleaks-install-hooks

GITLEAKS_VERSION ?= 8.18.2
GITLEAKS_BIN ?= $(shell command -v gitleaks 2>/dev/null || echo "")

gitleaks-scan:  ## Scan the full git history for secrets
	$(if $(GITLEAKS_BIN),,$(error gitleaks not found — install from https://github.com/gitleaks/gitleaks))
	gitleaks detect --source . --config .gitleaks.toml --verbose --no-banner

gitleaks-scan-staged:  ## Scan only staged changes (fast pre-commit check)
	$(if $(GITLEAKS_BIN),,$(error gitleaks not found — install from https://github.com/gitleaks/gitleaks))
	gitleaks protect --source . --config .gitleaks.toml --staged --verbose --no-banner

gitleaks-install-hooks:  ## Install gitleaks pre-commit hook
	scripts/install-gitleaks-hook.sh

# ── SBOM ──────────────────────────────────────────────────────────────────────

sbom-verify: sbom  ## Validate the generated SBOM
	@test -s "$(SBOM_FILE)" || { echo "FAIL: $(SBOM_FILE) not found or empty"; exit 1; }
	@test "$$(python3 -c "import json,sys; d=json.load(open('$(SBOM_FILE)')); sys.exit(0 if d.get('bomFormat')=='CycloneDX' else 1)")" \
		&& echo "PASS: valid CycloneDX SBOM" \
		|| { echo "FAIL: $(SBOM_FILE) is not valid CycloneDX"; exit 1; }
	@test "$$(python3 -c "import json,sys; d=json.load(open('$(SBOM_FILE)')); comps=d.get('components',[]); print(len(comps)); sys.exit(0 if len(comps)>0 else 1)")" \
		&& echo "PASS: SBOM contains components" \
		|| { echo "FAIL: SBOM has no components"; exit 1; }
