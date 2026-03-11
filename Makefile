# App name
APP_NAME=tsqlnotes

# Build directory
BUILD_DIR=bin

# Main entry point
MAIN_CMD=cmd/tsqlnotes/main.go

# Default target
.PHONY: all
all: build

# Build the application
.PHONY: build
build:
	@echo "Building $(APP_NAME)..."
	@mkdir -p $(BUILD_DIR)
	@go build -o $(BUILD_DIR)/$(APP_NAME) $(MAIN_CMD)
	@echo "Build complete: $(BUILD_DIR)/$(APP_NAME)"

# Run the application
.PHONY: run
run:
	@echo "Running $(APP_NAME)..."
	@go run $(MAIN_CMD)

# Clean build artifacts
.PHONY: clean
clean:
	@echo "Cleaning up..."
	@rm -rf $(BUILD_DIR)
	@rm -f tsqlnotes
	@echo "Clean complete."

# Format the code
.PHONY: fmt
fmt:
	@echo "Formatting Go code..."
	@go fmt ./...

# Run go vet
.PHONY: vet
vet:
	@echo "Running go vet..."
	@go vet ./...

# Run tests
.PHONY: test
test:
	@echo "Running tests..."
	@go test -v ./...

# Tidy dependencies
.PHONY: tidy
tidy:
	@echo "Tidying go modules..."
	@go mod tidy

# Install dependencies (if any tools are needed)
.PHONY: deps
deps:
	@echo "Downloading dependencies..."
	@go mod download

# Show help
.PHONY: help
help:
	@echo "Available targets:"
	@echo "  make build  - Build the executable"
	@echo "  make run    - Run the application directly"
	@echo "  make clean  - Remove build artifacts"
	@echo "  make fmt    - Format Go code"
	@echo "  make vet    - Run go vet"
	@echo "  make test   - Run tests"
	@echo "  make tidy   - Tidy modules"
	@echo "  make deps   - Download modules"
