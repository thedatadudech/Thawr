# Thawr build targets. See CLAUDE.md for the full command reference.

BINARY      := bin/thawr
PKG         := ./cmd/thawr
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS     := -s -w -X main.version=$(VERSION)
GO          ?= go
CONFIG      ?= config/server.example.yaml

export CGO_ENABLED = 0

.PHONY: all build test lint fmt vet integration run-server run-client proto clean

all: build

build:
	$(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(BINARY) $(PKG)

test:
	# The race runtime needs cgo; the shipped binary (make build) stays CGO_ENABLED=0.
	CGO_ENABLED=1 $(GO) test -race -count=1 ./...

lint: fmt vet
	golangci-lint run ./...

fmt:
	@out="$$(gofmt -l . 2>/dev/null)"; if [ -n "$$out" ]; then echo "gofmt needed on:"; echo "$$out"; exit 1; fi

vet:
	$(GO) vet ./...

# Integration tests need Linux with CAP_NET_ADMIN (network namespaces).
integration:
	CGO_ENABLED=1 $(GO) test -race -count=1 -tags integration ./tests/...

run-server: build
	$(BINARY) server --config $(CONFIG)

# THAWR_SERVER and THAWR_TOKEN come from `thawr admin token create` on the server.
run-client: build
	$(BINARY) client up --server $(THAWR_SERVER) --token $(THAWR_TOKEN)

# Regenerates gRPC and protobuf code. buf and the Go plugins run through
# `go run` with pinned versions, so nothing needs to be installed.
proto:
	cd internal/api/proto && $(GO) run github.com/bufbuild/buf/cmd/buf@v1.57.2 generate

clean:
	rm -rf bin dist
