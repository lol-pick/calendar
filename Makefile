.PHONY: build run test test-integration vet clean db-up db-down db-reset tidy

BIN_DIR := ./bin
BIN := calendar
PKG := ./...

GOLANGCI_VERSION ?= v2.1.6
GOLANGCI         := $(BIN_DIR)/golangci-lint

DATABASE_URL ?= postgres://calendar:calendar@localhost:5433/calendar?sslmode=disable

build:
	go build -o $(BIN) .

run: build
	./$(BIN) $(ARGS)

tidy:
	go mod tidy

test:
	go test -race -v ./...

test-integration: db-up
	@echo "waiting for postgres..."
	@until docker exec calendar-postgres pg_isready -U calendar >/dev/null 2>&1; do sleep 0.5; done
	DATABASE_URL_TEST="$(DATABASE_URL)" go test -race -v ./...

vet:
	go vet ./...

clean:
	rm -f $(BIN)

db-up:
	docker compose up -d postgres

db-down:
	docker compose down

db-reset:
	docker compose down -v
	docker compose up -d postgres

$(GOLANGCI):
	@mkdir -p $(BIN_DIR)
	GOBIN=$(abspath $(BIN_DIR)) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)

.PHONY: lint
lint: $(GOLANGCI)
	$(GOLANGCI) run $(PKG)
	$(GOLANGCI) fmt --diff $(PKG)

.PHONY: lint-fix
lint-fix: $(GOLANGCI) 
	$(GOLANGCI) fmt $(PKG)
