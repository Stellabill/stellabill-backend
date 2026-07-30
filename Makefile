GOPATH := $(shell go env GOPATH)
MUTEST := $(GOPATH)/bin/go-mutesting
GOFUMPT := $(GOPATH)/bin/gofumpt

# ── Proto / gRPC ──────────────────────────────────────────────────────────────

.PHONY: proto proto-lint proto-gen

proto: proto-gen proto-lint  ## Generate protos and run lint

proto-lint:  ## Lint proto definitions
	buf lint

proto-gen:  ## Generate Go stubs, grpc-gateway handlers, and OpenAPI specs from proto
	buf generate

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

# ── Mutation testing ──────────────────────────────────────────────────────────

.PHONY: mutation-state-machine
mutation-state-machine: $(MUTEST)  ## Run mutation tests on the subscription state machine
	$(MUTEST) ./internal/subscriptions/...

$(MUTEST):
	go install github.com/avito-tech/go-mutesting/cmd/go-mutesting@latest
