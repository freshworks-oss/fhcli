.DEFAULT_GOAL := build
.PHONY: build test snapshot install spec test-live

REVISION := $(shell cat git-revision 2>/dev/null || git rev-parse --short HEAD)
# The git tag is the only source of the version: releases are cut by pushing
# a vX.Y.Z tag (see RELEASING.md), and local builds describe themselves
# relative to the latest tag, e.g. 0.1.0-3-gabc1234-dirty.
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/^v//')

build:
	go build -ldflags "-X main.commit=$(REVISION) -X main.version=$(VERSION)" -o fhcli cmd/main.go

test:
	go vet ./...
	go test -race ./...

# Build the release archives locally without publishing (needs goreleaser v2).
snapshot:
	goreleaser release --snapshot --clean

install: build
	install -m 0755 fhcli $(shell go env GOPATH)/bin/fhcli

spec:
	curl -fsSL https://api.firehydrant.io/v1/swagger_doc -o pkg/spec/spec.json

# Run the test suite against the live API spec instead of the embedded
# snapshot, to catch spec changes that break `fhcli api` or `fhcli search`
# before running `make spec`.
test-live:
	@tmp=$$(mktemp -t fhcli-live-spec) && \
	curl -fsSL https://api.firehydrant.io/v1/swagger_doc -o $$tmp && \
	FHCLI_TEST_SPEC=$$tmp go test -count=1 ./... ; status=$$?; rm -f $$tmp; exit $$status
