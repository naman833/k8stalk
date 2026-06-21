BINARY_NAME=k8stalk
MODULE=github.com/naman833/k8stalk
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS=-ldflags "-s -w -X main.version=$(VERSION)"

.PHONY: all build test clean install lint

all: build

build:
	go build $(LDFLAGS) -o $(BINARY_NAME) .

install:
	go install $(LDFLAGS) .

test:
	go test ./... -v -race

test-short:
	go test ./... -short

lint:
	golangci-lint run ./...

clean:
	rm -f $(BINARY_NAME)
	go clean

# Development helpers
run-analyze:
	go run . analyze --filter=Pod

run-diagnose:
	go run . diagnose "what pods have issues?"

# Release
snapshot:
	goreleaser release --snapshot --clean

release:
	goreleaser release --clean

.DEFAULT_GOAL := build
