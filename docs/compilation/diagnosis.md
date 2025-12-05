# Diagnosis System Specification

The diagnosis system is responsible for collecting, formatting, and reporting errors and warnings encountered during the compilation process. It is designed to be user-friendly, providing context and actionable suggestions.

## Architecture

The system is centralized around the `internal/errors` package. A `Handler` instance is passed through the compilation pipeline, allowing each stage to report diagnostics without aborting immediately (where possible).

## Data Structures

### Diagnostic
A single unit of feedback.

*   **Severity**: `INFO`, `WARNING`, `ERROR`.
*   **Code**: A unique identifier (e.g., `E0123`) for documentation lookup.
*   **Message**: A concise description of the issue.
*   **Position**: File, Line, Column.
*   **Context**: The source code line where the error occurred.
*   **Highlights**: Specific ranges within the context to underline.
*   **Suggestion**: Optional actionable fix (e.g., "Did you mean `myVar`?").

### Handler
Collects diagnostics and manages the compilation state (e.g., determining if compilation should halt).

## Error Categories & Codes

Errors are categorized by the pipeline stage that detects them:

*   **Lexical/Syntax (P0000 - P0999)**: Parser errors (e.g., missing semicolon, invalid token).
*   **Resolution (R1000 - R1999)**: Name resolution errors (e.g., undefined variable, redeclaration).
*   **Type System (T2000 - T2999)**: Type mismatches, invalid casts, missing trait implementations.
*   **Control Flow (C3000 - C3999)**: Unreachable code, missing return statements.
*   **Limits (L4000 - L4999)**: Stack overflow, too many constants.

## Reporting Strategy

1.  **Non-Fatal Errors**: The compiler should attempt to recover and continue parsing/analyzing to report as many errors as possible in a single run.
    *   *Example*: If a statement is missing a semicolon, assume it's there and continue parsing the next statement.
2.  **Fatal Errors**: Issues that prevent further processing (e.g., file not found, internal compiler error). These halt the pipeline immediately.

## Output Format

Diagnostics are printed to `stderr` in a human-readable format by default, with support for structured output (JSON) for tools.

**Human-Readable Example:**
```text
[ERROR] E1001: undefined variable 'count'
  --> src/main.lum:10:5
   |
10 |     print(cont)
   |           ^^^^
   |
   = Suggestion: Did you mean 'count'?
```

## Integration Plan

1.  **Parser**: Enhance `internal/parser` to accept an `*errors.Handler`. Implement synchronization points for error recovery.
2.  **Resolver**: Update `internal/resolve` to report scope errors via the handler instead of returning simple errors.
3.  **Type Checker**: Update `internal/types` to report mismatches via the handler.
4.  **CLI**: Add flags for output format (`--format=json`) and verbosity.
