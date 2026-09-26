.PHONY: proto test

GO ?= go
BUF ?= buf

proto:
	$(BUF) generate

test:
	$(GO) test ./...
