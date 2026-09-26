.PHONY: docker-up docker-down docker-ps

# Interpolation reads the same files the services read through env_file: the committed example first
# and the local .env on top, so a value is defined once. Only existing files are passed — Compose
# fails on a missing --env-file. deploy/monitoring/.env.example is deliberately absent: its password
# is a placeholder, and the compose guard must fire until the real one is copied in.
COMPOSE_FILES := backend/cabby-gateway/.env.example backend/cabby-gateway/.env \
	backend/auth/.env.example backend/auth/.env deploy/monitoring/.env
COMPOSE := docker compose $(addprefix --env-file ,$(foreach f,$(COMPOSE_FILES),$(if $(wildcard $(f)),$(f))))

docker-up:
	$(COMPOSE) up --build -d

docker-down:
	$(COMPOSE) down

docker-ps:
	$(COMPOSE) ps
