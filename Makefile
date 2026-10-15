.PHONY: help build clean test test-unit test-e2e test-integration test-race bench fuzz run

# The go on the PATH, or where the official installer puts it
GO ?= $(shell command -v go 2>/dev/null || echo /usr/local/go/bin/go)

help:
	@echo "Lumen Makefile"
	@echo ""
	@echo "Usage:"
	@echo "  make build              Build the lumen binary"
	@echo "  make test               Run all tests (unit + e2e + integration)"
	@echo "  make test-unit          Run Go unit tests"
	@echo "  make test-e2e           Run end-to-end tests (lumen run on .lum files)"
	@echo "  make test-integration   Run integration tests (toolchain)"
	@echo "  make test-race          Run the tests that run programs under the race detector"
	@echo "  make bench              Measure the virtual machine, and whole programs"
	@echo "  make fuzz               Search for inputs that break Lumen (FUZZ_TIME=60s each)"
	@echo "  make clean              Clean build artifacts and test output"
	@echo "  make run                Run lumen CLI (shows help)"

build:
	@echo "Building lumen..."
	@$(GO) build -o lumen ./cmd/lumen
	@echo "Build complete: ./lumen"

# Every run of the tests has a time limit, and the packages that run
# programs end themselves if they come to hold too much memory (see
# internal/memguard): a test that goes wrong fails, it does not take the
# machine with it.
TEST_TIMEOUT := 300s

test-unit:
	@echo "Running unit tests..."
	@$(GO) test -timeout $(TEST_TIMEOUT) ./internal/...

test-e2e:
	@echo "Running end-to-end tests..."
	@$(GO) test -timeout $(TEST_TIMEOUT) ./test/e2e ./test/fuzz ./test/bench ./test/native

# The race detector also checks every conversion of a number into a
# pointer, which is how the VM keeps its objects in its values (see
# docs/luvm.md): that check is turned off, the rest is what is wanted.
test-race:
	@echo "Running tests under the race detector..."
	@$(GO) test -race -gcflags=all=-d=checkptr=0 -timeout 900s ./internal/vm ./internal/stdlib ./internal/memguard ./test/e2e

# The virtual machine on its own, then whole programs (test/bench)
bench:
	@$(GO) test ./internal/vm ./test/bench -run '^$$' -bench . -benchmem -timeout 600s

# Lets the machine look for inputs that break the compiler or the virtual
# machine, a minute each. What it finds is kept under test/fuzz/testdata
# and run with the tests from then on.
FUZZ_TIME := 60s

fuzz:
	@$(GO) test ./test/fuzz -run '^$$' -fuzz FuzzSource -fuzztime $(FUZZ_TIME) -parallel 4 -timeout 900s
	@$(GO) test ./test/fuzz -run '^$$' -fuzz FuzzBytecode -fuzztime $(FUZZ_TIME) -parallel 4 -timeout 900s


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
