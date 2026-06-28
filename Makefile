GOPATH := $(shell go env GOPATH)
MUTEST := $(GOPATH)/bin/go-mutesting

# ── Mutation testing ──────────────────────────────────────────────────────────

.PHONY: mutation-state-machine
mutation-state-machine: $(MUTEST)  ## Run mutation tests on the subscription state machine
	$(MUTEST) ./internal/subscriptions/...

$(MUTEST):
	go install github.com/avito-tech/go-mutesting/cmd/go-mutesting@latest
