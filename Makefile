.PHONY: docker-up docker-down docker-ps

# Interpolation reads the local .env of each service, the same files the services read through
# env_file. Only existing files are passed — Compose fails on a missing --env-file.
COMPOSE_FILES := backend/cabby-gateway/.env backend/auth/.env deploy/monitoring/.env
COMPOSE := docker compose $(addprefix --env-file ,$(foreach f,$(COMPOSE_FILES),$(if $(wildcard $(f)),$(f))))

docker-up:
	$(COMPOSE) up --build -d

docker-down:
	$(COMPOSE) down

docker-ps:
	$(COMPOSE) ps
