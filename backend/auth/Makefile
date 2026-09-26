.PHONY: run test test-race vet test-integration migrate

# The address given to make itself, captured before .env is read: an included value would
# otherwise beat the environment. Integration tests take their DSN only from here.
TEST_DB_URL := $(CABBY_AUTH_DB_URL)

# The committed example is the base: every value a fresh clone needs, so no target lists names and
# nothing is copied by hand. A local .env is read last and overrides only what differs on this
# machine; a value on the make command line overrides both. A bare `export` hands every value to the
# recipe process.
-include .env.example
-include .env
export

GO ?= go

run:
	$(GO) run ./cmd/auth

migrate:
	$(GO) run ./cmd/auth -migrate

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

# Integration tests run only against the DSN passed to make — never against .env, whose address
# resolves only inside the compose network. Without it every tagged file skips itself.
test-integration:
	CABBY_AUTH_DB_URL="$(TEST_DB_URL)" $(GO) test -tags integration ./...
