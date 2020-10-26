SHELL := bash
.ONESHELL:
.SHELLFLAGS := -eu -o pipefail -c
.DELETE_ON_ERROR:
MAKEFLAGS += --warn-undefined-variables
MAKEFLAGS += --no-builtin-rules

CMD = influx2hny
BIN = $(CURDIR)/bin
GO_SOURCES := cmd/$(CMD)/main.go $(wildcard *.go) go.mod go.sum

CIRCLE_TAG ?= 0.0.0
PACKAGES = dist/linux_amd64 dist/linux_arm64

# default target: will get you a debug build in the top-level directory.
# aliased as "make build"
.PHONY: build
build: $(CMD)
$(CMD): $(GO_SOURCES)
	go build -o $@ ./cmd/$(CMD)

# package: each target in $PACKGAES built into a directory under dist/
.PHONY: package
package: $(PACKAGES)
$(PACKAGES): dist/% : dist/%/$(CMD) dist/%/$(CMD).sha256

%.sha256: %
	sha256sum $* > $@

# builds the actual binary for each OS/ARCH in $PACKAGES
# this pattern matches something like /dist/linux_arm64/influx2hny
# $* is the matched pattern, which is split on '_' to get the GOOS & GOARCH values
# CIRCLE_TAG is set automatically in CI
dist/%/$(CMD): $(GO_SOURCES)
	GOOS=$(word 1,$(subst _, ,$*)) GOARCH=$(word 2,$(subst _, ,$*)) go build -trimpath -ldflags "-s -w -X main.BuildVersion=$(CIRCLE_TAG)" -o $@ ./cmd/$(CMD)

.PHONY: lint
lint: $(BIN)/golangci-lint
	$(BIN)/golangci-lint run ./...

$(BIN):
	mkdir -p $@

$(BIN)/golangci-lint: $(BIN)
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(BIN) v1.31.0

.PHONY: clean
clean:
	rm -f ./$(CMD)
	rm -rf dist
