# Testing Strategy

## Test Tiers
1. **Unit Tests**: Exercise each exported function, boundary conditions, error paths, and acceptance criteria literals.
2. **Property Tests**: Fixed seed tests via `testing/quick` and continuous fuzzing via `FuzzCompile` targets located in `testdata/fuzz`.
3. **Tagged Conformance**: Runs `//go:build conformance` tests. This downloads the WebAssembly/spec test suite at a pinned commit and WABT `wast2json`, verifying them with SHA-256 before running the MVP `.wast` files.

## Conformance exclusions

None.
