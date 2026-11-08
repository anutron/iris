.PHONY: build test fmt vet clean install-dev

GO ?= go
BIN_DIR := bin
BIN := $(BIN_DIR)/iris

# Builds bin/iris and signs it with a stable identity so the Keychain grant for
# the 1Password bootstrap survives rebuilds. See scripts/build-signed.sh.
build:
	@mkdir -p $(BIN_DIR)
	GO="$(GO)" IRIS_BUILD_OUT="$(BIN)" sh scripts/build-signed.sh

test:
	$(GO) test ./... -race -count=1

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

clean:
	rm -rf $(BIN_DIR)

install-dev: build
	cp $(BIN) $$HOME/bin/iris
