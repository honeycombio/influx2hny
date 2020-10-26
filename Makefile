SHELL := bash
.ONESHELL:
.SHELLFLAGS := -eu -o pipefail -c
.DELETE_ON_ERROR:
MAKEFLAGS += --warn-undefined-variables
MAKEFLAGS += --no-builtin-rules

CMD = influx2hny
BIN = $(CURDIR)/bin
GO_SOURCES := cmd/$(CMD)/main.go $(wildcard *.go) go.mod go.sum

# default target: will get you a debug build in the top-level directory.
# aliased as "make build"
.PHONY: build
build: $(CMD)
$(CMD): $(GO_SOURCES)
	go build -o $@ ./cmd/$(CMD)

#########################
### PACKAGE / RELEASE ###
#########################

PACKAGES = dist/linux_amd64 dist/linux_arm64

# package: each target in $PACKGAES built into a directory under dist/
.PHONY: package
package: $(PACKAGES)
$(PACKAGES): dist/% : dist/%/$(CMD) dist/%/$(CMD).sha256

%.sha256: %
	sha256sum $* > $@

CIRCLE_TAG ?=
RELEASE_VERSION ?= $(or $(CIRCLE_TAG), $(shell git rev-parse --short HEAD))
RELEASE_BUCKET ?= honeycomb-builds

# builds the actual binary for each OS/ARCH in $PACKAGES
# this pattern matches something like /dist/linux_arm64/influx2hny
# $* is the matched pattern, which is split on '_' to get the GOOS & GOARCH values
dist/%/$(CMD): $(GO_SOURCES)
	GOOS=$(word 1,$(subst _, ,$*)) GOARCH=$(word 2,$(subst _, ,$*)) go build -trimpath -ldflags "-s -w -X main.BuildVersion=$(RELEASE_VERSION)" -o $@ ./cmd/$(CMD)

.PHONY: release
release: $(PACKAGES)
	aws s3 sync dist/ s3://$(RELEASE_BUCKET)/honeycombio/influx2hny/$(RELEASE_VERSION)

###############
### TOOLING ###
###############

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
