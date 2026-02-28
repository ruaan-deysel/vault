BINARY=vault
VERSION=0.1.0
GOFLAGS=-ldflags "-s -w -X main.version=$(VERSION)"
GOOS=linux
GOARCH=amd64

.PHONY: build test clean package

build:
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS) -o $(BINARY)-$(GOOS)-$(GOARCH) ./cmd/vault/

build-local:
	go build -o $(BINARY) ./cmd/vault/

test:
	go test ./... -v

test-short:
	go test ./... -short

clean:
	rm -f $(BINARY) $(BINARY)-*-*

lint:
	golangci-lint run ./...

package: build
	@echo "Binary: $(BINARY)-$(GOOS)-$(GOARCH)"
	@echo "Plugin: plugin/vault.plg"
	@ls -lh $(BINARY)-$(GOOS)-$(GOARCH)
