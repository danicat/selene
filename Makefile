.PHONY: build test install clean release-dry-run

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

build:
	@go build -ldflags "-s -w -X main.version=$(VERSION)" -o bin/selene .

test:
	@go test -v ./...

install:
	@./install.sh

clean:
	@rm -rf bin/ dist/

release-dry-run:
	@goreleaser release --snapshot --clean
