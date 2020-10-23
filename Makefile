SHELL := bash
.ONESHELL:
.SHELLFLAGS := -eu -o pipefail -c
.DELETE_ON_ERROR:
MAKEFLAGS += --warn-undefined-variables
MAKEFLAGS += --no-builtin-rules

CMD = influx2hny
BIN = $(CURDIR)/bin
DIST = $(CURDIR)/dist
GO_SOURCES := cmd/$(CMD)/main.go $(wildcard *.go) go.mod go.sum

CIRCLE_TAG ?= 0.0.0

.PHONY: build
build: dist/influx2hny

dist/$(CMD): $(GO_SOURCES)
	go build -o $@ ./cmd/$(CMD)

PACKAGES = linux_amd64 linux_arm64
package: $(PACKAGES:%=dist/%)
$(PACKAGES:%=dist/%) : dist/% : dist/%/$(CMD) dist/%/$(CMD).sha256

%.sha256: %
	sha256sum $* > $@

dist/%/$(CMD) : $(GO_SOURCES)
	GOOS=$(word 1,$(subst _, ,$*)) GOARCH=$(word 2,$(subst _, ,$*)) go build -trimpath -ldflags "-s -w -X main.BuildVersion=$(CIRCLE_TAG)" -o $@ ./cmd/influx2hny

.PHONY: lint
lint: $(BIN)/golangci-lint
	$(BIN)/golangci-lint run ./...

$(BIN):
	mkdir -p $@

$(BIN)/golangci-lint: $(BIN)
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(BIN) v1.31.0

.PHONY: clean
clean:
	rm -rf dist
