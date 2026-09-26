.PHONY: run test test-race vet

# The committed example is the base: it carries every value a fresh clone needs, so no target lists
# names and nothing has to be copied by hand. A local .env is read last and overrides only what
# differs on this machine; values set on the make command line override both.
-include .env
# A bare `export` gives every value to the recipe process.
export

GO ?= go

run:
	$(GO) run ./cmd/cabby-gateway

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...
