# List available commands
default:
    @just --list

# Run the server in development mode
dev:
    go run ./cmd/giro

# Run with kiro-cli credentials (Linux)
run-linux:
    KIRO_CLI_DB_FILE="~/.local/share/kiro-cli/data.sqlite3" go run ./cmd/giro

# Run with kiro-cli credentials (macOS)
run-macos:
    KIRO_CLI_DB_FILE="~/Library/Application Support/kiro-cli/data.sqlite3" go run ./cmd/giro

# Build the binary
build:
    go build -o bin/giro ./cmd/giro

# Run tests with race detection
test:
    go test -race -count=1 ./...

# Run tests with coverage report
cover:
    go test -race -count=1 -coverprofile=coverage.out ./...
    go tool cover -html=coverage.out -o coverage.html

# Run mock end-to-end tests
e2e-mock:
    go test -count=1 -tags=e2e_mock ./test/e2e/...

# Run real end-to-end smoke tests (requires credentials env vars)
e2e-real:
    go test -count=1 -tags=e2e_real ./test/e2e/...

# Format code with gofumpt
fmt:
    gofumpt -w .

# Run linter
lint:
    golangci-lint run

# Run linter with auto-fix
lint-fix:
    golangci-lint run --fix

# Format, lint, and test
check: fmt lint test

# Tidy and verify dependencies
tidy:
    go mod tidy && go mod verify

# Remove build artifacts
clean:
    rm -rf bin/ coverage.out coverage.html
