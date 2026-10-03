.PHONY: test test-race vet check

# One entry point for every backend module. Each module keeps its own Makefile and go.mod; this file
# only runs the same target in all of them and stops at the first failure. Integration tests need a
# database and stay in `make -C auth test-integration`.
MODULES := contracts lifecycle platform auth location cabby-gateway

test test-race vet:
	@for module in $(MODULES); do \
		echo "==> $$module: $@"; \
		$(MAKE) --no-print-directory -C $$module $@ || exit 1; \
	done

# check is what must be green before a change is done (AGENTS.md).
check: test test-race vet
