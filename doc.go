// Package wasminterpreter is a small, dependency-free Go library: Zero-dependency WebAssembly MVP binary decoder, validator, and stack-based interpreter with explicit fuel metering and host imports.
//
// The public API is every exported identifier of this root package. Functions are pure: they do
// no I/O, keep no global mutable state and report invalid input through returned errors.
package wasminterpreter
