.PHONY: run test test-race vet

GO ?= go

# ARGS goes to the emulator as is: make run ARGS="-cabbers 1 -interval 1s -duration 1m".
# The module has no database and no .env, so there is nothing to migrate or to integration-test.
run:
	$(GO) run ./cmd/emulator $(ARGS)

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...
