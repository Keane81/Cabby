.PHONY: run test test-race vet test-integration

# The addresses given to make itself, captured before .env is read: an included value would
# otherwise beat the environment. Integration tests take their DSNs only from here.
TEST_AUTH_DB_URL := $(CABBY_DBVIEWER_AUTH_DB_URL)
TEST_LOCATION_DB_URL := $(CABBY_DBVIEWER_LOCATION_DB_URL)

# Only the local .env is read, not the committed example: its DSNs name the compose services
# `auth-db` and `location-db`, which resolve only inside the compose network, so a host run needs
# its own values. A bare `export` hands every value to the recipe process.
-include .env
export

GO ?= go

run:
	$(GO) run ./cmd/dbviewer

# The UI helpers are plain ES modules and are tested with Node's built-in runner; without node the
# step is skipped with a message rather than failing the Go checks.
test:
	$(GO) test ./...
	@if command -v node >/dev/null 2>&1; then \
		node --test internal/ui/format.test.mjs; \
	else \
		echo "node not found: skipping the UI helper tests"; \
	fi

test-race:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

# Integration tests run only against the DSNs passed to make — never against .env, whose addresses
# resolve only inside the compose network. They create and drop tables named dbv_it_* in the
# `public` schema, so point them at a throwaway database. Without a DSN every tagged file skips.
test-integration:
	CABBY_DBVIEWER_AUTH_DB_URL="$(TEST_AUTH_DB_URL)" CABBY_DBVIEWER_LOCATION_DB_URL="$(TEST_LOCATION_DB_URL)" \
		$(GO) test -tags integration ./...
