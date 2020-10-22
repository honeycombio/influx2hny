SHELL := bash
.ONESHELL:
.SHELLFLAGS := -eu -o pipefail -c
.DELETE_ON_ERROR:
MAKEFLAGS += --warn-undefined-variables
MAKEFLAGS += --no-builtin-rules

BIN = $(CURDIR)/bin

.PHONY: all
all: dist/influx2hny

dist/influx2hny: cmd/influx2hny/main.go *.go go.mod go.sum
	go build -o $@ $<

.PHONY: lint
lint: $(BIN)/golangci-lint
	$(BIN)/golangci-lint run ./...

$(BIN):
	mkdir -p $@

$(BIN)/golangci-lint: $(BIN)
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(BIN) v1.31.0

clean:
	rm -rf $(BIN) dist/influx2hny
