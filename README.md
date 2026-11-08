# wasm-interpreter

Go applications needing to run untrusted plugin code often face heavy CGO dependencies or unsafe unbounded execution. Without fuel metering and strict memory sandboxing, untrusted WebAssembly binaries can exhaust host memory and CPU cycles. This library provides a pure Go WebAssembly MVP runtime with deterministic fuel accounting and configurable memory limits.

## Installation

`go get github.com/Trysac/u403-wasm-interpreter`

## Usage

```go
package main

import "github.com/Trysac/u403-wasm-interpreter"

func main() {
	mod, err := wasminterpreter.Compile(wasmBytes, wasminterpreter.DefaultConfig())
	if err != nil { panic(err) }
	inst, err := wasminterpreter.Instantiate(mod, nil)
	if err != nil { panic(err) }
	res, err := inst.Call("add", wasminterpreter.I32(20), wasminterpreter.I32(22))
}
```

## Public API

- `type Config`: Contains `MaxMemoryPages`, `MaxStackDepth`, `MaxCallDepth`, `MaxModuleBytes`, and `Fuel`.
- `func Compile(wasm []byte, cfg Config) (*Module, error)`: Decodes and validates Wasm modules.
- `func Instantiate(mod *Module, imports map[string]map[string]HostFunc) (*Instance, error)`: Links and prepares for execution.
- `func (inst *Instance) Call(funcName string, args ...Value) ([]Value, error)`: Executes an exported function.
- `func (inst *Instance) Memory() []byte`: Returns the linear memory buffer.
- `func (inst *Instance) RemainingFuel() uint64`: Returns remaining execution cycles.

### Errors and Limits
- Errors use a `Code` string: `invalid_magic`, `invalid_version`, `malformed_section`, `validation_failed`, `limit_exceeded`, `export_not_found`, and trap codes like `integer_overflow` or `out_of_fuel`.
- Default `MaxModuleBytes` is 16 MiB, `MaxMemoryPages` is 256, `MaxStackDepth` is 65536, and `MaxCallDepth` is 1024.

## Why not Wazero?

Wazero is a large JIT compiler and complex runtime; this library is a pure Go, zero-dependency, self-contained educational MVP interpreter prioritizing determinism, lightweight embedding, and strict fuel metering.

See [docs/SECURITY.md](docs/SECURITY.md) for vulnerability reporting.
