.PHONY: docker-up docker-up-clean docker-down docker-ps smoke test test-race vet check

# Backend checks run through backend/Makefile, which covers every module.
test test-race vet check:
	$(MAKE) -C backend $@

# Interpolation reads the local .env of each service, the same files the services read through
# env_file. Only existing files are passed — Compose fails on a missing --env-file.
COMPOSE_FILES := backend/cabby-gateway/.env backend/auth/.env backend/location/.env deploy/monitoring/.env
COMPOSE := docker compose $(addprefix --env-file ,$(foreach f,$(COMPOSE_FILES),$(if $(wildcard $(f)),$(f))))

docker-up:
	$(COMPOSE) up --build -d

docker-down:
	$(COMPOSE) down

docker-ps:
	$(COMPOSE) ps

# Start on empty databases: no cabbers, no sessions, no locations. Only the volumes of the two
# service databases are removed; Prometheus and Grafana keep their history.
docker-up-clean:
	$(COMPOSE) down
	@project=$$($(COMPOSE) config | sed -n 's/^name: //p'); \
	for v in auth-data location-data; do \
		docker volume rm -f $$(docker volume ls -q \
			--filter label=com.docker.compose.project=$$project \
			--filter label=com.docker.compose.volume=$$v); \
	done
	$(COMPOSE) up --build -d

# End-to-end check of a running stack through the gateway (Bruno collection, folder smoke).
# Each run registers its own cabber, so it passes on an empty and on a used database alike.
SMOKE_ENV ?= LOCAL
SMOKE_DIR := backend/bruno/cabby-gateway
smoke:
	@host=$$(sed -n 's/^ *host: *//p' $(SMOKE_DIR)/environments/$(SMOKE_ENV).bru); \
	echo "waiting for $$host/healthz"; \
	for i in $$(seq 1 30); do curl -fs $$host/healthz >/dev/null && break; sleep 1; done
	cd $(SMOKE_DIR) && npx --yes @usebruno/cli run smoke --env $(SMOKE_ENV)
