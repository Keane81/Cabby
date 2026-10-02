.PHONY: run test test-race vet test-integration migrate

# The address given to make itself, captured before .env is read: an included value would
# otherwise beat the environment. Integration tests take their DSN only from here.
TEST_DB_URL := $(CABBY_LOCATION_DB_URL)

# Only the local .env is read, not the committed example: its DSN names the compose service
# `location-db`, which resolves only inside the compose network, so a host run needs its own values.
# Copy .env.example to .env and adjust it before the first `make run`; a value on the make command
# line overrides the file. A bare `export` hands every value to the recipe process.
-include .env
export

GO ?= go

run:
	$(GO) run ./cmd/location

migrate:
	$(GO) run ./cmd/location -migrate

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

# Integration tests run only against the DSN passed to make — never against .env, whose address
# resolves only inside the compose network. Without it every tagged file skips itself.
test-integration:
	CABBY_LOCATION_DB_URL="$(TEST_DB_URL)" $(GO) test -tags integration ./...
