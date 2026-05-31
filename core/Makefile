GO ?= go

.PHONY: fmt-check vet test cover build ci

fmt-check:
	@test -z "$$(find . -name '*.go' -print0 | xargs -0 gofmt -l)" || \
		(echo "Go files are not formatted. Run 'gofmt -w' on the files above."; \
		find . -name '*.go' -print0 | xargs -0 gofmt -l; exit 1)

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

cover:
	$(GO) test -covermode=atomic -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out

build:
	$(GO) build ./...

ci: fmt-check vet test build
