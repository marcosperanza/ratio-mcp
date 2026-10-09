.PHONY: build test check

build:
	go build -trimpath -o bin/ratio-mcp ./cmd/ratio-mcp

test:
	go test ./...

check:
	gofmt -w $$(find . -name '*.go' -type f)
	go vet ./...
	go test ./...
