.PHONY: help build clean test test-unit test-e2e test-integration run

# Go binary location
GO := /usr/local/go/bin/go

help:
	@echo "Lumen Makefile"
	@echo ""
	@echo "Usage:"
	@echo "  make build              Build the lumen binary"
	@echo "  make test               Run all tests (unit + e2e + integration)"
	@echo "  make test-unit          Run Go unit tests"
	@echo "  make test-e2e           Run end-to-end tests (lumen run on .lum files)"
	@echo "  make test-integration   Run integration tests (toolchain)"
	@echo "  make clean              Clean build artifacts and test output"
	@echo "  make run                Run lumen CLI (shows help)"

build:
	@echo "Building lumen..."
	@$(GO) build -o lumen ./cmd/lumen
	@echo "Build complete: ./lumen"

test-unit:
	@echo "Running unit tests..."
	@$(GO) test ./internal/...

test-e2e:
	@echo "Running end-to-end tests..."
	@$(GO) test ./test/e2e


test-integration:
	@echo "Running integration tests..."
	@./test/integration/test.sh

test: test-unit test-e2e test-integration
	@echo ""
	@echo "All tests completed successfully!"

clean:
	@echo "Cleaning build artifacts..."
	@rm -f lumen
	@rm -f cmd/lumen/lumen
	@rm -rf test/output
	@rm -f *.lc
	@rm -f examples/*.lc
	@find . -name "*.test" -delete
	@echo "Clean complete"

# Run the CLI
run: build
	@./lumen
