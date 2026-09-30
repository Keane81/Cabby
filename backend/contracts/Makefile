.PHONY: proto test test-race vet

GO ?= go
BUF ?= buf

proto:
	$(BUF) generate

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...
