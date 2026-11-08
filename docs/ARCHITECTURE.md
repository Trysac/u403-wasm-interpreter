# Architecture

## Design Overview

`wasminterpreter` is designed with the following principles:
- **Zero External Dependencies**: Pure standard library Go only.
- **Deterministic Fuel Accounting**: Every executed instruction decrements fuel; identical inputs and fuel yield exact identical execution counts.
- **Explicit Stack and Control Flow**: Control blocks (`block`, `loop`, `if`) and call frames use explicit Go slices rather than Go recursion, preventing host stack overflow.
- **Canonical NaNs**: IEEE 754 float operations canonize NaN bit patterns as specified in WebAssembly section 4.3.3.
