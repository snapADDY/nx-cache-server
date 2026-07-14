.DEFAULT_GOAL := build

.PHONY: build
build:
	CGO_ENABLED=0 go build -trimpath -o bin/nx-cache-server ./cmd/nx-cache-server

.PHONY: test
test:
	go test -race -cover ./...

.PHONY: lint
lint:
	golangci-lint run

.PHONY: fmt
fmt:
	golangci-lint fmt

.PHONY: generate
generate:
	go generate ./...

# Multi-arch build; stays in the build cache. Add --push (and a registry
# tag) to publish, or use docker-load for a locally runnable image.
.PHONY: docker
docker:
	docker buildx build --platform linux/amd64,linux/arm64 -t nx-cache-server:local .

.PHONY: clean
clean:
	rm -rf bin
