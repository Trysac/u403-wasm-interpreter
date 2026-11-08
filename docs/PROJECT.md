# Project Record

## Objective
To provide a pure Go WebAssembly MVP 1.0 binary decoder, validator, and stack-based interpreter with explicit fuel metering and host imports.

## Scope
- WebAssembly MVP 1.0 binary format decoding (type, import, func, table, mem, global, export, start, elem, code, data, custom sections).
- Semantic validation.
- Explicit interpreter loop with fuel accounting.
- Integer and IEEE 754 float operations.
- Linear memory load/store with bounds checks.
- Direct/indirect calls.
- Explicit value and control stack (no Go recursion per Wasm call).
- NaN results canonical per spec section 4.3.3.
- Runner for the wast2json JSON command format.

## Out-of-Scope
- Post-MVP proposals (SIMD, threads, GC, multi-memory, tail calls, component model).
- JIT compilation.
- Full WASI OS emulation (only minimal host func example provided).

## Line comparison metrics

| Model | Line | Fix Rounds | Input Tokens | Output Tokens | Cost | Time | Failing Checks Per Round | Conformance Percentage |
|---|---|---|---|---|---|---|---|---|
pending measurement | pending measurement | pending measurement | pending measurement | pending measurement | pending measurement | pending measurement | pending measurement | pending measurement |

## Factory run u403 (measured from API usage)

| Phase | Model | Batch | Input | Output | Cache write | Cache read | Cost (USD) |
|---|---|---|---|---|---|---|---|
| analyst | gemini-3.8-flash | no | 3441 | 1146 | 0 | 0 | 0.0069 |
| generation | gemini-3.8-flash | yes | 5172 | 46886 | 0 | 0 | 0.0899 |
| fix-1 | gemini-3.8-flash | no | 80066 | 1677 | 0 | 718050 | 0.1202 |
| fix-2 | gemini-3.8-flash | no | 241245 | 1506 | 0 | 3141424 | 0.4222 |
| fix-3 | gemini-3.8-flash | no | 216587 | 1528 | 0 | 3276858 | 0.4139 |
| fix-4 | gemini-3.8-flash | no | 182254 | 1740 | 0 | 3503478 | 0.4060 |
| fix-5 | gemini-3.8-flash | no | 180680 | 1669 | 0 | 3309392 | 0.3900 |
| fix-6 | gemini-3.8-flash | no | 212697 | 1648 | 0 | 3407970 | 0.4213 |
| fix-1 | gemini-3.8-flash | no | 172570 | 4501 | 0 | 3264448 | 0.3911 |
| fix-2 | gemini-3.8-flash | no | 157610 | 15864 | 0 | 2314810 | 0.3513 |
| docs | gemini-3.1-flash-lite | no | 3778 | 1334 | 0 | 0 | 0.0029 |

Total: $3.0157. Prices: Anthropic list prices.
