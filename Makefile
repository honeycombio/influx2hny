SHELL := bash
.ONESHELL:
.SHELLFLAGS := -eu -o pipefail -c
.DELETE_ON_ERROR:
MAKEFLAGS += --warn-undefined-variables
MAKEFLAGS += --no-builtin-rules

CMD = influx2hny
BIN = $(CURDIR)/bin
GO_SOURCES := cmd/$(CMD)/main.go $(wildcard *.go) go.mod go.sum

.PHONY: build
#: default target: will get you a debug build in the top-level directory.
build: $(CMD)
$(CMD): $(GO_SOURCES)
	go build -o $@ ./cmd/$(CMD)

.PHONY: lint
#: run the linter
lint: $(BIN)/golangci-lint
	$(BIN)/golangci-lint run ./...

#########################
### PACKAGE / RELEASE ###
#########################

# Which operating systems and architectures will we cross-compile for?
# In the form of <os>-<arch>
OSARCHS=\
	linux-amd64   \
	linux-arm64   \

artifacts:
	@mkdir -p artifacts

# each item in OSARCHS appended to "artifacts/influx2hny-" to
# generate the list of file paths for binaries to be cross-compiled
BINARIES=$(OSARCHS:%=artifacts/$(CMD)-%)

# these targets set OS and ARCH variables for any target that matches a path in $(BINARIES)
# ex: "artifacts/influx2hny-linux-arm64"
# 0: "artifacts/influx2hny"
# 1: "linux"
# 2: "arm64"
# $* is the matched pattern, which is split on '-' to get the OS & ARCH values
$(BINARIES): OS = $(word 1,$(subst -, ,$*))
$(BINARIES): ARCH = $(word 2,$(subst -, ,$*))

# builds the actual binary for each OS/ARCH listed in $OSARCHS
$(BINARIES): artifacts/$(CMD)-%: artifacts
	@echo "building: $@"
	GOOS=$(OS) GOARCH=$(ARCH) \
	  go build -trimpath \
	           -ldflags "-s -w -X main.BuildVersion=$(RELEASE_VERSION)" \
	           -o $@ \
	           ./cmd/$(CMD)

.PHONY: checksums
checksums: artifacts/checksums.txt
artifacts/checksums.txt: $(BINARIES)
	@cd artifacts && shasum -a256 * > checksums.txt
	@echo "checksums for builds saved to $@"

.PHONY: package
#: cross-compile for each OS and ARCH, find binaries and checksums in artifacts/
package: $(BINARIES) checksums

CIRCLE_TAG ?=
RELEASE_VERSION ?= $(or $(CIRCLE_TAG), $(shell git rev-parse --short HEAD))
RELEASE_BUCKET ?= honeycomb-builds

.PHONY: release
release: package
	aws s3 sync artifacts/ s3://$(RELEASE_BUCKET)/honeycombio/influx2hny/$(RELEASE_VERSION)

###############
### TOOLING ###
###############

$(BIN):
	mkdir -p $@

$(BIN)/golangci-lint: $(BIN)
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(BIN) v1.31.0

.PHONY: clean
#: clean up
clean:
	rm -f ./$(CMD)
	rm -rf dist
	rm -rf artifacts
