# Closures and Upvalues Specification

This document describes the internal implementation of closures and upvalue management in the Lumen virtual machine.

## Overview

A **closure** is a function instance that captures variables from its lexical environment (the parent scope). These captured variables are called **upvalues**.

Lumen implements closures using an approach similar to Lua 5.1:
- Functions are first-class objects.
- Upvalues can be **open** (pointing to the stack) or **closed** (copied to the heap).
- The VM automatically manages the closing of upvalues when variables go out of scope.

## Object Structure

### FunctionObj (Closure)

In the heap, a function is represented by `FunctionObj`. To support closures, this object contains a list of upvalues:

```go
type FunctionObj struct {
    header   ObjHeader
    chunk    *Chunk
    protoIdx int
    upvalues []*UpvalueObj // List of captured variables
}
```

### UpvalueObj

An upvalue is an intermediate object that points to the actual value.

```go
type UpvalueObj struct {
    header   ObjHeader
    location *Value // Pointer to stack slot (nil when closed)
    closed   Value  // Closed-over value (used when location == nil)
}
```

- **Open Upvalue**: `location` points to a register in the VM stack (`&vm.regs[i]`).
- **Closed Upvalue**: `location` is `nil`. The value is stored in the `closed` field.

## Bytecode Instructions

The following instructions handle closure creation and upvalue manipulation:

### 1. NEWFUNC `A B`
Creates a new closure from a function prototype.
- **A**: Destination register.
- **B**: Prototype index (`protoIdx`).

This instruction allocates a new `FunctionObj`. The number of upvalues to allocate is determined by the `UpvalueCount` field in the function prototype.

### 2. CAPLOCAL `A B C`
Captures a local variable from the current stack frame into a closure's upvalue list.
- **A**: Destination closure register (must be a `FunctionObj`).
- **B**: Index of the upvalue in the closure's upvalue list.
- **C**: Register index of the local variable in the current stack frame.

This instruction finds or creates an open upvalue pointing to `vm.regs[C]` and stores it in `closure.upvalues[B]`.

### 3. CAPUPVAL `A B C`
Captures an existing upvalue from the current function (parent scope) into a new closure.
- **A**: Destination closure register.
- **B**: Index of the upvalue in the new closure's upvalue list.
- **C**: Index of the upvalue in the *current* function's upvalue list.

This instruction copies the `UpvalueObj` pointer from `currentFunc.upvalues[C]` to `newClosure.upvalues[B]`.

### 4. LDCAP `A B` (Load Capture)
Loads the value of an upvalue into a register.
- **A**: Destination register.
- **B**: Index of the upvalue in the current closure.

### 5. STCAP `A B` (Store Capture)
Stores the value of a register into an upvalue.
- **A**: Index of the upvalue.
- **B**: Source register.

## Upvalue Lifecycle

1. **Creation (Open)**:
   When `CAPLOCAL` is executed:
   - The VM checks the `openUpvalues` list to see if an upvalue already exists for the target register.
   - If yes, it reuses it (sharing).
   - If no, it creates a new "open" upvalue pointing to the stack register and adds it to the list.

2. **Access**:
   `LDCAP` and `STCAP` instructions read/write via the `UpvalueObj`.
   - If open (`location != nil`), they access the stack register directly.
   - If closed (`location == nil`), they access the `closed` field.

3. **Closing (Close)**:
   When a function returns (`RET`), its stack frame is about to be destroyed. The VM calls `closeUpvalues()`:
   - It iterates through the `openUpvalues` list.
   - For any upvalue pointing to a register in the dying stack frame:
     - The value is copied from the stack to the `closed` field.
     - The `location` pointer is set to `nil`.
     - The upvalue is removed from the `openUpvalues` list.

## Example Workflow

```assembly
; Function that returns a closure
.func makeCounter 1 0 0
    LOADK r0 0          ; r0 = count (local variable)
    
    ; Create closure 'inc' (prototype 0)
    ; 'inc' expects 1 upvalue
    NEWFUNC r1 0        
    
    ; Capture r0 as upvalue 0 of r1
    CAPLOCAL r1 0 r0    
    
    RET r1
.endfunc

; The closure function
.func inc 1 0 1         ; 1 reg, 0 args, 1 upvalue
    ; Load upvalue 0 into r0
    LDCAP r0 0          
    
    ; Increment
    ADD r0 r0 1         
    
    ; Store back to upvalue 0
    STCAP 0 r0          
    
    RET r0
.endfunc
```
