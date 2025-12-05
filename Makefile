.PHONY: help build clean test test-vm test-asm test-bytecode test-integration test-all run

# Go binary location
GO := /usr/local/go/bin/go

help:
	@echo "Lumen Makefile"
	@echo ""
	@echo "Usage:"
	@echo "  make build              Build the lumen binary"
	@echo "  make test               Run Go unit tests"
	@echo "  make test-integration   Run integration tests (toolchain)"
	@echo "  make test-all           Run all tests (unit + integration)"
	@echo "  make clean              Clean build artifacts and test output"
	@echo "  make run                Run lumen CLI (shows help)"

build:
	@echo "Building lumen..."
	@$(GO) build -o lumen ./cmd/lumen
	@echo "Build complete: ./lumen"

test-unit:
	@echo "Running unit tests..."
	@$(GO) test ./internal/vm ./internal/asm ./internal/types  ./internal/ir  ./internal/ast ./internal/resolve -v


test-integration:
	@echo "Running integration tests..."
	@./test/integration/test.sh

test: test-unit test-integration
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
