.PHONY: all test test-unit test-integration lint build docker-up docker-down migrate clean help update-mod-go

GO_VERSION := 1.24
GO_TOOLCHAIN := go1.25.4
GO_MOD_DIRS := . driver/pgxv5 driver/databasesql

all: lint test build

help:
	@echo "Available targets:"
	@echo "  make test             - Run all tests"
	@echo "  make test-unit        - Run unit tests only"
	@echo "  make test-integration - Run integration tests (requires Docker)"
	@echo "  make lint             - Run golangci-lint"
	@echo "  make build            - Build the module"
	@echo "  make docker-up        - Start PostgreSQL container"
	@echo "  make docker-down      - Stop PostgreSQL container"
	@echo "  make migrate          - Run database migrations"
	@echo "  make clean            - Clean build artifacts"

test: test-unit test-integration

test-unit:
	go test -v -race -short ./...

test-integration:
	go test -v -race -run Integration ./...

test-coverage:
	go test -v -race -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -html=coverage.out -o coverage.html

lint:
	@echo "Installing golangci-lint..." && GOTOOLCHAIN=local go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
	@for dir in $(GO_MOD_DIRS); do \
		echo "Linting $$dir..."; \
		(cd $$dir && PATH="$(PATH):$(shell go env GOPATH)/bin" golangci-lint run ./...) || exit 1; \
	done

build:
	go build ./...

docker-up:
	docker-compose up -d postgres
	@echo "Waiting for PostgreSQL..."
	@sleep 3

docker-down:
	docker-compose down

migrate:
	@which migrate > /dev/null || go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
	migrate -path storage/migrations -database "$(DATABASE_URL)" up

migrate-down:
	migrate -path storage/migrations -database "$(DATABASE_URL)" down 1

clean:
	go clean ./...
	rm -f coverage.out coverage.html

fmt:
	gofmt -s -w .

update-mod-go:
	@for dir in $(GO_MOD_DIRS); do \
		if [ "$(CHECK)" = "true" ]; then \
			grep -q "^go $(GO_VERSION)$$" $$dir/go.mod || (echo "go.mod in $$dir has wrong go version (expected $(GO_VERSION))" && exit 1); \
			grep -q "^toolchain $(GO_TOOLCHAIN)$$" $$dir/go.mod || (echo "go.mod in $$dir has wrong toolchain (expected $(GO_TOOLCHAIN))" && exit 1); \
		else \
			cd $$dir && go mod edit -go=$(GO_VERSION) -toolchain=$(GO_TOOLCHAIN) && cd - > /dev/null; \
		fi \
	done
	@if [ "$(CHECK)" = "true" ]; then echo "All go.mod files have correct go/toolchain directives"; fi
