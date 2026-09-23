.PHONY: docker-up docker-down docker-ps

COMPOSE := docker compose --env-file backend/cabby-gateway/.env --env-file deploy/monitoring/.env

docker-up:
	$(COMPOSE) up --build -d

docker-down:
	$(COMPOSE) down

docker-ps:
	$(COMPOSE) ps
