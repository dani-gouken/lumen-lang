# Lumen Debugger (lumdbg)

The `lumdbg` debugger is an interactive command-line tool for debugging Lumen programs written in assembly (.lasm) or compiled to bytecode (.lc).

## Usage

```bash
lumen lumdbg <file.lasm|file.lc>
```

## Features

### Interactive Debugging

The debugger provides step-by-step execution with a rich command set:

- **Step-by-step execution**: Execute one instruction at a time
- **Continue mode**: Run until completion or breakpoint
- **Breakpoint management**: Set, list, and remove breakpoints
- **Register inspection**: View all register values and their types
- **Source mapping**: See the original source file and line number for each instruction
- **Heap monitoring**: Track heap allocation and usage
- **Frame tracking**: Monitor call stack depth
- **Function labels**: View and navigate to function entry points

### Commands

#### Execution Control
- `s`, `step`, or Enter: Execute one instruction and pause
- `c`, `continue`: Continue execution until next breakpoint or completion
- `q`, `quit`, `exit`: Exit the debugger

#### Breakpoint Management
- `b <ip>`: Set breakpoint at instruction pointer (e.g., `b 10`)
- `b list`, `b ls`, `b l`: List all active breakpoints
- `b clear`, `b c`: Clear all breakpoints
- `b del <ip>`, `b delete <ip>`: Delete breakpoint at specific IP

#### Inspection
- `r`, `regs`, `registers`: Show all registers
- `r <n>`: Show specific register (e.g., `r 5`)
- `i`, `info`: Show detailed instruction and VM state
- `l`, `labels`: List all function labels with their IPs

#### Help
- `h`, `help`: Show command help

### Breakpoints in Source Files

You can set breakpoints directly in your `.lasm` files using the `;BREAK` comment directive:

```assembly
.code
  ;BREAK  ; Breakpoint will be set here
  LOADK r0 0

loop:
  ;BREAK  ; Breakpoint at loop entry
  ADD r0 r0 r1
  JMPIF r3 loop
```

When you load a file with `;BREAK` comments, the debugger will:
1. Automatically load all marked breakpoints
2. Display them with their locations
3. Stop execution at these points (in continue mode)

The `;BREAK` comment can appear on the line before an instruction, and the breakpoint will be set at that instruction.

### Display Information

For each instruction, the debugger displays:

- **Breakpoint indicator**: 🔴 if at a breakpoint
- **Instruction number** and **IP** (instruction pointer)
- **Function name** (if inside a function)
- **Source location**: 📄 file path and line:column
- **Source line**: The actual line of code from the .lasm file
- **Instruction**: ⚙️  The bytecode instruction being executed
- **VM state**: 📊 Active call frames and heap usage
- **Registers**: 💾 First 6 registers in compact form

### Value Inspection

The debugger can inspect and display:
- **Integers**: `Int(42)`
- **Numbers**: `Number(3.14)`
- **Booleans**: `Bool(true)` or `Bool(false)`
- **Null**: `Null`
- **Strings**: `String("hello")` (truncated if > 50 chars)
- **Arrays**: `Array([1i, 2i, 3i] len=3)` (first 5 elements shown)
- **Objects**: `Object({"x": 10i, "y": 20i})` (first 5 fields shown)

## Example Session

```bash
$ ./lumen lumdbg examples/debugger_demo.lasm
╔═══════════════════════════════════════════════════════════════════╗
║                      Lumen Debugger                               ║
╚═══════════════════════════════════════════════════════════════════╝

Commands:
  s, step              Step to next instruction
  c, continue          Continue until breakpoint or end
  b <ip>               Set breakpoint at instruction pointer
  b list               List all breakpoints
  ...

📍 Loading 3 breakpoint(s) from source file:
   🔴 IP 0 at examples/debugger_demo.lasm:14
   🔴 IP 2 (in loop) at examples/debugger_demo.lasm:19
   🔴 IP 7 (in loop) at examples/debugger_demo.lasm:28


─────────────────────────────────────────────────────────────────
🔴 Breakpoint | Instruction #1 | IP: 0
📄 examples/debugger_demo.lasm:14:1
   LOADK r0 0        ; Load counter = 0
⚙️  LOADK r0 r0
📊 Frames: 0 | Heap: 0/1048576 bytes (0.0% used)
💾 Registers: r0=Null | r1=Null | r2=Null | r3=Null | r4=Null

> s

─────────────────────────────────────────────────────────────────
Instruction #2 | IP: 1
📄 examples/debugger_demo.lasm:15:1
   LOADK r1 2        ; Load limit = 5
⚙️  LOADK r1 r2
📊 Frames: 0 | Heap: 0/1048576 bytes (0.0% used)
💾 Registers: r0=Int(0) | r1=Null | r2=Null | r3=Null | r4=Null

> b list
Breakpoints (3):
  🔴 IP 0 at examples/debugger_demo.lasm:14
  🔴 IP 2 (in loop) at examples/debugger_demo.lasm:19
  🔴 IP 7 (in loop) at examples/debugger_demo.lasm:28

> c
▶️  Continuing... (will stop at 3 breakpoint(s))

🔴 Breakpoint hit at IP 2
...

> r
All Registers (5):
  r0   = Int(0)
  r1   = Int(5)
  r2   = Null
  r3   = Null
  r4   = Null

> i
═══ Detailed Instruction Info ═══
Instruction Pointer: 2 / 9
Opcode: OpLOADK
Operands: A=2, B=1, C=0
...

> q
👋 Debugger exiting.
```

## Architecture

The debugger is built on several components:

### Component Structure

1. **`internal/debugger/state.go`** - State management
   - `VMSnapshot`: Complete capture of VM state
   - `ExecutionHistory`: Circular buffer of 1000 snapshots (for future history navigation)

2. **`internal/debugger/breakpoints.go`** - Breakpoint management
   - Support for breakpoints by instruction pointer (IP)
   - Support for breakpoints by label (function names)
   - Listing and removal of breakpoints

3. **`internal/debugger/inspector.go`** - Value inspection
   - `InspectValue()`: Detailed inspection of all value types
   - Support for heap objects with dereferencing
   - Formatted output for complex types

4. **`cmd/lumen/main.go`** - Command implementation
   - `cmdDebug()`: Main debugger loop
   - Interactive command processing
   - Integration with VM debug hooks
   - Breakpoint command handling

5. **`internal/vm/vm.go`** - VM debugger support
   - `debugHook`: Hook called before each instruction
   - `Snapshot()`: Captures complete VM state
   - No performance impact when debug hook is not set

6. **`internal/asm/assembler.go`** - Breakpoint directive support
   - Detects `;BREAK` comments during assembly
   - Marks instruction pointers in chunk metadata
   - Preserves breakpoints through serialization

7. **`internal/vm/chunk.go`** - Chunk metadata
   - `Breakpoints` field stores source-level breakpoints
   - Persists through bytecode serialization

## Debug Information

The debugger uses debug information embedded in the bytecode:
- Source file path
- Line and column numbers
- Original source line text
- Function name
- Description (from comments)

This information is displayed during execution to help with debugging.

## Tips and Best Practices

### Setting Breakpoints

1. **Use ;BREAK in source files** for important points you always want to inspect:
   ```assembly
   loop:
     ;BREAK  ; Debug loop entry
     ADD r0 r0 r1
   ```

2. **Set breakpoints interactively** for ad-hoc debugging:
   ```
   > b 15       ; Set at specific IP
   > b list     ; Check what's set
   > c          ; Continue to breakpoint
   ```

3. **Use labels to find breakpoints**: Run `l` to list all functions and their IPs, then set breakpoints at function entries

### Efficient Debugging Workflow

1. **Start with ;BREAK markers** at critical points (function entries, loop starts, error handlers)
2. **Use continue mode** (`c`) to quickly reach breakpoints
3. **Step through** (`s`) when you need fine-grained control
4. **Inspect registers** (`r`) to see intermediate values
5. **Use detailed info** (`i`) to understand opcode behavior
6. **Remove breakpoints** (`b clear`) once you've found the issue

### Understanding Execution

- **Watch the IP**: Track how control flow moves through your code
- **Monitor registers**: See how values change instruction by instruction
- **Check frame count**: Understand recursion depth and function calls
- **Watch heap usage**: Identify memory allocation patterns

## Current Limitations

1. No execution history navigation (backward/forward)
2. No call frame inspection (can't navigate up/down call stack)
3. No conditional breakpoints (can't break on conditions)
4. No watch expressions
5. No memory/heap visualization
6. Breakpoints set interactively are not saved

## Future Enhancements

- [ ] Execution history navigation (`↑`/`↓` arrows to step backward/forward)
- [ ] Call frame navigation (`up`, `down` commands)
- [ ] Conditional breakpoints (`b <ip> if r0 > 10`)
- [ ] Watch expressions (`watch r5`, auto-display on each step)
- [ ] Save/load debug sessions
- [ ] Replay mode from execution history
- [ ] Memory visualization (heap map, object graph)
- [ ] Performance profiling integration
- [ ] TUI (Terminal User Interface) with split panes
- [ ] Breakpoint persistence (save to file)
- [ ] Data breakpoints (break on memory change)

## Advanced Features

### Function Labels

The debugger extracts function labels from debug info. Use the `l` command to see all available functions:

```
> l
Function Labels (3):
  📍 loop                  (IP 2)
  📍 factorial             (IP 10)
  📍 helper                (IP 25)

Tip: Use 'b <ip>' to set a breakpoint at a function entry
```

### Detailed Instruction Info

The `i` command shows comprehensive information about the current instruction:

```
> i
═══ Detailed Instruction Info ═══
Instruction Pointer: 5 / 20
Opcode: OpADD
Operands: A=3, B=0, C=1

Debug Information:
  File: examples/test.lasm
  Line: 15, Column: 3
  Function: calculate
  Source:   ADD r3 r0 r1      ; sum = a + b

VM State:
  Stack Pointer: 0
  Frame Pointer: 0
  Frame Count: 1
  Register Count: 8
  Argument Count: 2

Heap State:
  Allocated: 0 bytes
  Maximum: 1048576 bytes
  Usage: 0.00%

Constants Pool: 5 entries
```

### Register Inspection

View specific registers or all at once:

```
> r 3
r3 = Int(42)

> r
All Registers (8):
  r0   = Int(10)
  r1   = Int(32)
  r2   = Null
  r3   = Int(42)
  r4   = String("hello")
  r5   = Array([1i, 2i, 3i] len=3)
  r6   = Object({"x": 5i, "y": 10i})
  r7   = Null
```

## Troubleshooting

### Breakpoints Not Hitting

- Verify breakpoint is set: `b list`
- Check IP range: Instruction must actually execute
- Ensure you're in continue mode: Use `c` not `s`

### Can't Find Function

- List all labels: `l`
- Check debug info is present: `i`
- Verify assembly file has function labels (e.g., `loop:`)

### Register Values Unexpected

- Step slowly through instructions: Use `s` repeatedly
- Watch register changes: Note which instructions modify registers
- Check instruction operands: Use `i` to see A, B, C values

## See Also

- [Assembly Language Reference](assembly.md)
- [VM Architecture](vm-architecture.md)
- [Function Calling Convention](function-calling-convention.md)
