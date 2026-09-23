.PHONY: run test test-race vet

-include .env

GO ?= go
CABBY_GATEWAY_PORT ?= 8080

run:
	CABBY_GATEWAY_PORT=$(CABBY_GATEWAY_PORT) $(GO) run ./cmd/cabby-gateway

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...
