# List available commands
default:
    @just --list

# Run the server in development mode
dev:
    go run ./cmd/giro

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
