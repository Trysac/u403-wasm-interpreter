# Security notes

## Threat model

`wasm-interpreter` is a library without I/O. 

- **Defended**: Host memory exhaustion is mitigated by `MaxMemoryPages` and `MaxModuleBytes`. CPU exhaustion is mitigated by deterministic `Fuel` metering. Host stack overflows are prevented by using explicit Go slices for Wasm stacks rather than Go recursion.
- **Not defended**: This library does not provide a sandboxed environment for execution of arbitrary host functions; the host is responsible for the security of any provided imports. It is not designed to execute malicious untrusted code in a multi-tenant environment without external process isolation.

To report a vulnerability, please contact the maintainers directly.
