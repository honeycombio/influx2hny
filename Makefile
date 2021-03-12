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
###     PACKAGING     ###
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
	@echo "+++ building: $@"
	GOOS=$(OS) GOARCH=$(ARCH) \
	  go build -trimpath \
	           -ldflags "-s -w -X main.BuildVersion=$(RELEASE_VERSION)" \
	           -o $@ \
	           ./cmd/$(CMD)

.PHONY: checksums
checksums: artifacts/checksums.txt
artifacts/checksums.txt: $(BINARIES)
	@cd artifacts && shasum -a256 * > checksums.txt
	@echo " * checksums for builds saved to $@"

.PHONY: package
#: cross-compile for each OS and ARCH, find binaries and checksums in artifacts/
package: $(BINARIES) checksums

#########################
###     RELEASES      ###
#########################

CIRCLE_TAG ?=
RELEASE_VERSION ?= $(or $(CIRCLE_TAG), $(shell git describe --tags))
RELEASE_BUCKET ?= honeycomb-builds

.PHONY: release
#: perform all the publish_* tasks
release: publish_github publish_s3

.PHONY: publish_github
#: draft a GitHub release for current commit/tag and upload builds as its assets
publish_github: github_prereqs package
	@echo "+++ publishing these assets to GitHub, tag $(RELEASE_VERSION)"
	@ls -l artifacts/*
	@cat artifacts/checksums.txt
	@ghr -draft \
	     -name ${RELEASE_VERSION} \
	     -token ${GITHUB_TOKEN} \
	     -username ${CIRCLE_PROJECT_USERNAME} \
	     -repository ${CIRCLE_PROJECT_REPONAME} \
	     -commitish ${CIRCLE_SHA1} \
	     ${RELEASE_VERSION} \
	     ~/artifacts

.PHONY: github_prereqs
github_prereqs: ghr_present
	@:$(call check_defined, RELEASE_VERSION, the tag from which to create this release)
	@:$(call check_defined, GITHUB_TOKEN, auth to create this release)
	@:$(call check_defined, CIRCLE_PROJECT_USERNAME, user who will create this release)
	@:$(call check_defined, CIRCLE_PROJECT_REPONAME, the repository getting a new release)
	@:$(call check_defined, CIRCLE_SHA1, the git ref to associate with this release)

.PHONY: publish_s3
#: upload builds to the S3 bucket defined as RELEASE_BUCKET
publish_s3: s3_prereqs package
	@echo "+++ publishing these artfacts to $(RELEASE_BUCKET) bucket, version $(RELEASE_VERSION)"
	@ls -l artifacts/*
	@cat artifacts/checksums.txt
	@aws s3 sync artifacts/ s3://$(RELEASE_BUCKET)/honeycombio/influx2hny/$(RELEASE_VERSION)

.PHONY: s3_prereqs
s3_prereqs: awscli_present
	@:$(call check_defined, RELEASE_BUCKET, name of bucket to receive uploaded artifacts)
	@:$(call check_defined, RELEASE_VERSION, the version number of this release)

###############
### TOOLING ###
###############

$(BIN):
	mkdir -p $@

$(BIN)/golangci-lint: $(BIN)
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(BIN) v1.31.0

.PHONY: ghr_present
ghr_present:
	@which ghr || (echo "ghr missing; required to create release at GitHub"; exit 1)

.PHONY: awscli_present
awscli_present:
	@which aws || (echo "aws cli missing; required to push builds to S3"; exit 1)

.PHONY: clean
#: clean up
clean:
	rm -f ./$(CMD)
	rm -rf dist
	rm -rf artifacts

check_defined = \
    $(strip $(foreach 1,$1, \
        $(call __check_defined,$1,$(strip $(value 2)))))
__check_defined = \
    $(if $(value $1),, \
        $(error Undefined $1$(if $2, ($2))$(if $(value @), \
                required by target `$@')))
