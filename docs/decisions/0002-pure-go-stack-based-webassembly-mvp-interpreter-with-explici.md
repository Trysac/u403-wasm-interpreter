# Pure Go Stack-Based WebAssembly MVP Interpreter with Explicit Control and Fuel Accounting

Status: accepted (generated, pending human review)

## Context

Applications requiring sandboxed plugin execution in pure Go need predictable resource bounds (memory limits and instruction fuel) without native CGO dependencies or JIT complexity.

## Decision

Implement an explicit interpreter loop with an operand stack and control frames, strictly bounds-checked memory loads/stores, and single-unit fuel metering for each WebAssembly bytecode instruction.

## Consequences

Provides guaranteed deterministic execution, sandbox isolation from host crashes, and zero runtime dependencies, while operating at interpreter rather than JIT execution speeds.
