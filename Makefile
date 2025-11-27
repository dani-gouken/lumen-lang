.PHONY: help build clean test test-vm test-asm test-bytecode test-integration test-all run

# Go binary location
GO := /usr/local/go/bin/go

# Default target
help:
	@echo "Lumen Makefile"
	@echo ""
	@echo "Usage:"
	@echo "  make build              Build the lumen binary"
	@echo "  make test               Run Go unit tests"
	@echo "  make test-vm            Run VM unit tests only"
	@echo "  make test-asm           Run assembler/disassembler tests only"
	@echo "  make test-bytecode      Run bytecode serialization tests only"
	@echo "  make test-integration   Run integration tests (toolchain)"
	@echo "  make test-all           Run all tests (unit + integration)"
	@echo "  make clean              Clean build artifacts and test output"
	@echo "  make run                Run lumen CLI (shows help)"

# Build the binary
build:
	@echo "Building lumen..."
	@$(GO) build -o lumen ./cmd/lumen
	@echo "Build complete: ./lumen"

# Run all Go unit tests
test:
	@echo "Running unit tests..."
	@$(GO) test ./internal/vm ./internal/asm ./internal/debugger -v

# Run VM tests only
test-vm:
	@echo "Running VM tests..."
	@$(GO) test ./internal/vm -v

# Run assembler/disassembler tests only
test-asm:
	@echo "Running assembler/disassembler tests..."
	@$(GO) test ./internal/asm -v

# Run bytecode serialization tests only
test-bytecode:
	@echo "Running bytecode serialization tests..."
	@$(GO) test ./internal/vm -v -run 'Bytecode|Serialize|Deserialize'

# Run integration tests using go run
test-integration:
	@echo "Running integration tests..."
	@./test/integration/test.sh

# Run integration tests using built binary
test-integration-binary: build
	@echo "Running integration tests with binary..."
	@USE_BINARY=1 ./test/integration/test.sh

# Run all tests
test-all: test test-integration
	@echo ""
	@echo "All tests completed successfully!"

# Clean build artifacts and test output
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
