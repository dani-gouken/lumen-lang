# Memory Management

Lumen uses a simple mark-and-sweep garbage collector to manage heap-allocated objects. This document describes the memory management system and how to use it.

## Overview

The memory management system consists of:

1. **Fixed-size heap** - Pre-allocated memory pool (1MB by default)
2. **NaN-boxed values** - Efficient 64-bit value representation with heap pointers
3. **Mark-sweep GC** - Simple, stop-the-world garbage collection
4. **Five heap object types** - String, Array, Object/Map, Function, and Upvalue

## Heap Architecture

### Heap Structure

The heap is a fixed-size memory region that tracks all allocated objects:

```go
type Heap struct {
    objects        *ObjHeader  // Linked list of all objects
    bytesAllocated uint64      // Current memory usage
    nextGC         uint64      // GC trigger threshold
    maxBytes       uint64      // Heap size limit
}
```

**Default configuration:**
- Heap size: 1MB (1024 * 1024 bytes)
- GC threshold: 80% of heap size
- Allocation tracking: Per-object size accounting

### Object Layout

All heap objects start with a common header:

```go
type ObjHeader struct {
    Type   ObjType     // String, Array, or Object
    Marked bool        // GC mark flag
    Next   *ObjHeader  // Next object in linked list
}
```

Objects are linked together in a singly-linked list for easy traversal during garbage collection.

## Heap Object Types

### 1. String Objects

Immutable strings stored on the heap:

```go
type StringObj struct {
    header ObjHeader
    data   string
}
```

**Operations:**
- `AllocString(s string)` - Allocate a new string
- `Data()` - Get string contents

**Example:**
```go
str, shouldGC, err := vm.heap.AllocString("hello world")
if err != nil {
    // Handle allocation failure
}
```

### 2. Array Objects

Dynamic arrays that can grow:

```go
type ArrayObj struct {
    header   ObjHeader
    elements []Value
}
```

**Operations:**
- `AllocArray(capacity int)` - Allocate array with initial capacity
- `Get(i int)` - Get element at index (returns null if out of bounds)
- `Set(i int, v Value)` - Set element at index (grows array if needed)
- `Append(v Value)` - Append element to end
- `Len()` - Get current length

**Example:**
```go
arr, shouldGC, err := vm.heap.AllocArray(10)
arr.Append(NewInt(1))
arr.Append(NewInt(2))
arr.Set(0, NewInt(42))
val := arr.Get(0)  // Returns 42
```

### 3. Object/Map

Hash table with string keys:

```go
type ObjectObj struct {
    header ObjHeader
    fields map[string]Value
}
```

**Operations:**
- `AllocObject()` - Allocate empty object
- `Get(key string)` - Get value by key (returns value and bool)
- `Set(key string, v Value)` - Set key-value pair
- `Keys()` - Get all keys as slice

**Example:**
```go
obj, shouldGC, err := vm.heap.AllocObject()
obj.Set("name", NewInt(123))
obj.Set("age", NewInt(25))
val, ok := obj.Get("name")  // Returns 123, true
```

### 4. Function Objects

First-class functions and closures:

```go
type FunctionObj struct {
    header   ObjHeader
    chunk    *Chunk
    protoIdx int
    upvalues []*UpvalueObj
}
```

**Operations:**
- `AllocFunction(chunk *Chunk, protoIdx int)` - Allocate a new function closure

### 5. Upvalue Objects

Captured variables for closures:

```go
type UpvalueObj struct {
    header   ObjHeader
    location *Value
    closed   Value
}
```

**Operations:**
- `AllocUpvalue(location *Value)` - Allocate a new upvalue

## Garbage Collection

### Mark-Sweep Algorithm

Lumen uses a classic two-phase mark-and-sweep garbage collector:

#### Phase 1: Mark

Starting from **GC roots** (registers and stack), recursively mark all reachable objects:

```go
func (vm *VM) markRoots() {
    // Mark objects in registers
    for i := 0; i < len(vm.regs); i++ {
        vm.markValue(vm.regs[i])
    }

    // Mark objects on stack
    for i := 0; i < vm.sp; i++ {
        vm.markValue(vm.stack[i])
    }
}
```

The marking process:
1. Check if value is a heap pointer
2. Skip if already marked (prevents cycles)
3. Set `Marked = true` on the object header
4. Recursively mark children (array elements, object fields)

#### Phase 2: Sweep

Walk the object list and free unmarked objects:

```go
func (h *Heap) sweep() {
    current := h.objects
    for current != nil {
        if current.Marked {
            // Keep object, clear flag for next cycle
            current.Marked = false
        } else {
            // Free unreachable object
            freeObject(current)
        }
        current = current.Next
    }
}
```

### GC Triggers

Garbage collection can be triggered in three ways:

#### 1. Automatic Trigger

After any allocation, check if threshold is exceeded:

```go
obj, shouldGC, err := vm.heap.AllocArray(10)
if shouldGC {
    vm.CollectGarbage()  // Automatically triggered
}
```

Threshold formula: `bytesAllocated >= maxBytes * 0.8`

#### 2. Manual Trigger

Use the `GC` opcode or call directly:

```go
vm.CollectGarbage()
```

#### 3. Allocation Failure

When heap is full and allocation fails, you might run GC and retry (not automatic).

### GC Roots

Objects reachable from these locations are never collected:

1. **Registers** - Current function's register file
2. **Stack** - All values on the VM stack
3. **Constants** - String constants in the constant pool (if stored as heap pointers)

### Performance Characteristics

**Stop-the-world GC:**
- Pauses execution during collection
- Predictable behavior
- Simple to implement and debug

**Time complexity:**
- Mark phase: O(reachable objects + edges)
- Sweep phase: O(total objects)

**Space overhead:**
- 1 bit per object (marked flag)
- Pointer per object (linked list)

## Bytecode Operations

### Allocation Opcodes

#### NEWARRAY
Allocate an array with specified capacity:

```
NEWARRAY r0 10    ; r0 = new array with capacity 10
```

Format: `NEWARRAY dest capacity`

#### NEWOBJ
Allocate an empty object/map:

```
NEWOBJ r0         ; r0 = new object
```

Format: `NEWOBJ dest`

#### NEWSTR
Load a string constant (strings pre-allocated in constant pool):

```
NEWSTR r0 0       ; r0 = string from constant[0]
```

Format: `NEWSTR dest const_index`

### Array Operations

#### GETINDEX
Get array element:

```
GETINDEX r2 r0 r1  ; r2 = r0[r1]
```

Format: `GETINDEX dest array index`

#### SETINDEX
Set array element:

```
SETINDEX r0 r1 r2  ; r0[r1] = r2
```

Format: `SETINDEX array index value`

#### ARRLEN
Get array length:

```
ARRLEN r1 r0       ; r1 = len(r0)
```

Format: `ARRLEN dest array`

### Object Operations

#### GETFIELD
Get object field:

```
GETFIELD r2 r0 0   ; r2 = r0[constant[0]]
```

Format: `GETFIELD dest object field_name_const_index`

Field name must be a string constant in the constant pool.

#### SETFIELD
Set object field:

```
SETFIELD r0 0 r1   ; r0[constant[0]] = r1
```

Format: `SETFIELD object field_name_const_index value`

### Garbage Collection

#### GC
Manually trigger garbage collection:

```
GC                 ; Run garbage collection
```

Format: `GC`

## Examples

### Example 1: Simple Array

Assembly code:

```asm
; Create array and populate it
NEWARRAY r0 3          ; r0 = new array(3)
LOADK r1 0             ; r1 = 10
LOADK r2 1             ; r2 = 0
SETINDEX r0 r2 r1      ; r0[0] = 10

LOADK r1 2             ; r1 = 20
LOADK r2 3             ; r2 = 1
SETINDEX r0 r2 r1      ; r0[1] = 20

LOADK r2 3             ; r2 = 1
GETINDEX r3 r0 r2      ; r3 = r0[1]
RET r3                 ; return 20
```

Constants: `[10, 0, 20, 1]`

### Example 2: Object with Fields

Assembly code:

```asm
; Create object and set fields
NEWOBJ r0              ; r0 = new object
LOADK r1 2             ; r1 = 123
SETFIELD r0 0 r1       ; r0["name"] = 123

LOADK r1 3             ; r1 = 25
SETFIELD r0 1 r1       ; r0["age"] = 25

GETFIELD r2 r0 1       ; r2 = r0["age"]
RET r2                 ; return 25
```

Constants: `["name", "age", 123, 25]` (as heap pointers)

### Example 3: Nested Structures

Create an object with an array field:

```asm
NEWOBJ r0              ; r0 = new object
NEWARRAY r1 3          ; r1 = new array(3)

LOADK r2 1             ; r2 = 100
LOADK r3 2             ; r3 = 0
SETINDEX r1 r3 r2      ; r1[0] = 100

SETFIELD r0 0 r1       ; r0["data"] = r1

GETFIELD r4 r0 0       ; r4 = r0["data"]
LOADK r3 2             ; r3 = 0
GETINDEX r5 r4 r3      ; r5 = r4[0]
RET r5                 ; return 100
```

Constants: `["data", 100, 0]`

### Example 4: Manual GC

Allocate garbage and trigger collection:

```asm
NEWARRAY r0 10         ; r0 = array (garbage)
NEWARRAY r0 10         ; r0 = array (overwrites, making previous garbage)
NEWOBJ r1              ; r1 = object (garbage)
GC                     ; Collect unreachable objects
LOADK r2 0             ; r2 = 42
RET r2                 ; return 42
```

After `GC`, the first array and object are collected since nothing references them.

## Memory Usage Tracking

### Query Heap Statistics

```go
vm := NewVM()

// Get current memory usage
bytes := vm.heap.BytesAllocated()

// Get heap limit
maxBytes := vm.heap.MaxBytes()

// Check if GC should run
shouldGC := vm.heap.ShouldCollect()
```

### Custom Heap Size

Create VM with custom heap size:

```go
// 10KB heap for testing
vm := NewVMWithHeapSize(10 * 1024)

// Or 10MB heap for production
vm := NewVMWithHeapSize(10 * 1024 * 1024)
```

## Best Practices

### 1. Minimize Allocations

Reuse objects when possible:

```asm
; Bad: Creates new array each iteration
LOOP:
    NEWARRAY r0 10
    ; ... use array ...
    JMP LOOP

; Better: Reuse array
NEWARRAY r0 10
LOOP:
    ; ... use same array ...
    JMP LOOP
```

### 2. Keep Live Set Small

Don't hold references to objects you don't need:

```asm
; Bad: Holds all arrays
NEWARRAY r0 10
NEWARRAY r1 10
NEWARRAY r2 10
; All 3 arrays stay alive

; Better: Overwrite registers
NEWARRAY r0 10
; ... use r0 ...
NEWARRAY r0 10    ; First array can be collected
; ... use new r0 ...
```

### 3. Monitor Heap Usage

Check `ShouldCollect()` and adjust allocation patterns if GC runs too frequently.

### 4. Test with Small Heaps

Use small heap sizes during testing to catch memory leaks:

```go
vm := NewVMWithHeapSize(1024)  // 1KB - very tight
```

## Limitations

Current implementation has these limitations:

1. **No heap growth** - Fixed size, allocation fails when full
2. **No compaction** - Fragmentation can reduce available memory
3. **Stop-the-world** - GC pauses execution
4. **Simple allocator** - Not optimized for performance
5. **No weak references** - All reachable objects are kept
6. **No finalizers** - No cleanup hooks when objects are freed

## Future Improvements

Potential enhancements:

- **Growing heap** - Expand heap when needed
- **Generational GC** - Separate young/old generations
- **Incremental GC** - Reduce pause times
- **Compaction** - Reduce fragmentation
- **Better allocator** - Free lists, size classes, etc.
- **Write barriers** - Enable incremental/concurrent GC
- **GC statistics** - Collection counts, pause times, etc.

## Debugging Tips

### Count Objects

```go
func countObjects(h *Heap) int {
    count := 0
    current := h.objects
    for current != nil {
        count++
        current = current.Next
    }
    return count
}
```

### Print Heap Stats

```go
fmt.Printf("Heap: %d / %d bytes (%.1f%%)\n",
    heap.BytesAllocated(),
    heap.MaxBytes(),
    float64(heap.BytesAllocated())/float64(heap.MaxBytes())*100)
```

### Verify GC Correctness

```go
// Allocate objects
obj1 := heap.AllocObject()
obj2 := heap.AllocString("test")

// Keep only obj1 in registers
vm.regs[0] = NewHeapPtr(uintptr(unsafe.Pointer(obj1)))

// Run GC
vm.CollectGarbage()

// obj1 should survive, obj2 should be collected
```

## Summary

Lumen's memory management provides:
- ✓ Simple, predictable garbage collection
- ✓ Three essential heap object types
- ✓ Automatic and manual GC triggering
- ✓ Fixed-size heap with overflow detection
- ✓ Comprehensive bytecode operations

The design prioritizes simplicity and debuggability over performance, making it ideal for learning and prototyping language implementations.
