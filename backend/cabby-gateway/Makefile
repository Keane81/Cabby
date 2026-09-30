.PHONY: run test test-race vet

# Only the local .env is read, not the committed example: copy .env.example to .env before the first
# run, or CABBY_AUTH_ADDR stays unset and the service refuses to start. Values set on the make command
# line override the file.
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
