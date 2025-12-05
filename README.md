# Lumen Programming Language

Lumen is a prototype programming language with a focus on simplicity and efficiency.

## What is Lumen?

Lumen is a statically-typed programming language currently in early development. This repository contains the core compiler infrastructure and runtime.

For the surface language syntax and semantics, see [docs/language-spec.md](docs/language-spec.md) (draft).

## What is luvm?

luvm (Lumen Virtual Machine) is the bytecode interpreter for Lumen. It features:

- Register and stack hybrid execution model
- NaN-boxed value representation for efficient type handling
- Mark-sweep garbage collector
- Compact bytecode format

See [docs/luvm.md](docs/luvm.md) for detailed VM specification.

## Current Status

This is a prototype implementation. The following components are implemented:

- ✅ luvm bytecode virtual machine
- ✅ NaN-boxed value representation
- ✅ Opcode definitions and execution
- ✅ Binary bytecode format (.lc files)
- ✅ Assembler (text → bytecode)
- ✅ Disassembler (bytecode → text)
- ✅ Arithmetic operations (ADD, SUB, MUL, DIV)
- ✅ CLI interface with asm/disasm/exec commands
- ⚠️ Parser (stub only)
- ⚠️ Type checker (stub only)
- ⚠️ Code generation (stub only)

## Repository Structure

```
lumen/
  cmd/lumen/       - CLI entry point
  docs/            - Documentation
  examples/        - Example assembly files
  internal/
    vm/            - Virtual machine implementation
    asm/           - Assembler and disassembler
    ast/           - AST definitions (stub)
    parser/        - Parser (stub)
    types/         - Type system (stub)
```

## Building and Running

```bash
# Initialize dependencies
go mod tidy

# Build the project
go build ./...

# Run the CLI
go run ./cmd/lumen
```

## Working with Bytecode

### Assembly Format

Lumen assembly (.lasm) uses a simple text format:

```assembly
.constants 2
  0: 10.0        ; floating point number
  1: 5i          ; integer (48-bit)

.func main 3 0
  LOADK r0 0     ; load constant into register
  LOADK r1 1
  ADD r2 r0 r1   ; r2 = r0 + r1
  RET r2         ; return result
.endfunc
```

### Value Types

- **Numbers**: `42.5`, `3.14159`
- **Integers**: `42i`, `-100i` (48-bit signed)
- **Booleans**: `true`, `false`
- **Null**: `null`

### Available Commands

```bash
# Assemble .lasm to .lc bytecode
go run ./cmd/lumen asm input.lasm output.lc

# Disassemble .lc bytecode to .lasm text
go run ./cmd/lumen disasm input.lc output.lasm

# Execute .lc bytecode
go run ./cmd/lumen exec program.lc

# Example workflow
go run ./cmd/lumen asm examples/add.lasm add.lc
go run ./cmd/lumen exec add.lc
# Output: Result: Number(30)
```

### Examples

See the [examples/](examples/) directory for sample assembly programs:

- **add.lasm** - Simple addition
- **complex.lasm** - Multiple arithmetic operations
- **types.lasm** - Various value types

## Testing

Lumen has a two-level testing approach. See [docs/testing.md](docs/testing.md) for philosophy and details.

```bash
# Run all tests (unit + integration)
make test-all

# Run only Go unit tests
make test

# Run only integration tests
make test-integration

# Run specific test suites
make test-vm          # VM tests only
make test-asm         # Assembler/disassembler tests only
make test-bytecode    # Bytecode serialization tests only
```

Test output is written to `test/output/` (gitignored).

## Requirements

- Go 1.22 or later

## License

MIT
