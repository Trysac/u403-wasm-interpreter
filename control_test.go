package wasminterpreter_test

import (
	"errors"
	"math/rand"
	"testing"

	"github.com/Trysac/u403-wasm-interpreter"
)

// newRand returns a deterministic random source for property tests.
func newRand() *rand.Rand {
	return rand.New(rand.NewSource(403))
}

// sumBody is the body of sum(n) = 0 + 1 + ... + n using block, loop, br_if and br.
func sumBody() []byte {
	return body([]local{{2, tI32}},
		block(tEmpty),
		loop(tEmpty),
		lget(2), lget(0), op(0x4A), brIf(1), // i > n: exit the block
		lget(1), lget(2), op(0x6A), lset(1), // sum += i
		lget(2), i32c(1), op(0x6A), lset(2), // i++
		br(0),
		end,
		end,
		lget(1),
		end,
	)
}

// i32Func builds a module exporting "f"(i32) -> i32 with the given body code.
func i32Func(locals []local, code ...[]byte) []byte {
	return singleFunc([]byte{tI32}, []byte{tI32}, locals, false, code...)
}

func callI32(t *testing.T, wasm []byte, arg int32) int32 {
	t.Helper()
	inst := instantiate(t, wasm, wasminterpreter.DefaultConfig(), nil)
	res := mustCall(t, inst, "f", wasminterpreter.I32(arg))
	if len(res) != 1 || res[0].Typ != wasminterpreter.ValTypeI32 {
		t.Fatalf("expected one i32 result, got %v", res)
	}
	return res[0].I32
}

func TestIfElse(t *testing.T) {
	wasm := i32Func(nil, lget(0), ifOp(tI32), i32c(10), op(0x05), i32c(20), end, end)
	if got := callI32(t, wasm, 1); got != 10 {
		t.Fatalf("if true: expected 10, got %d", got)
	}
	if got := callI32(t, wasm, 0); got != 20 {
		t.Fatalf("if false: expected 20, got %d", got)
	}

	// if without else
	noElse := i32Func([]local{{1, tI32}},
		i32c(5), lset(1),
		lget(0), ifOp(tEmpty), i32c(7), lset(1), end,
		lget(1), end)
	if got := callI32(t, noElse, 1); got != 7 {
		t.Fatalf("if without else, true: expected 7, got %d", got)
	}
	if got := callI32(t, noElse, 0); got != 5 {
		t.Fatalf("if without else, false: expected 5, got %d", got)
	}
}

func TestLoopAndBranch(t *testing.T) {
	wasm := wasmModule{
		types:   [][]byte{funcType([]byte{tI32}, []byte{tI32})},
		funcs:   []uint32{0},
		exports: [][]byte{exportFunc("sum", 0)},
		codes:   [][]byte{sumBody()},
	}.bytes()
	inst := instantiate(t, wasm, wasminterpreter.DefaultConfig(), nil)
	for n, want := range map[int32]int32{0: 0, 1: 1, 10: 55, 100: 5050} {
		res := mustCall(t, inst, "sum", wasminterpreter.I32(n))
		if res[0].I32 != want {
			t.Fatalf("sum(%d): expected %d, got %v", n, want, res)
		}
	}
}

func TestBlockResultAndBranchValue(t *testing.T) {
	// block (result i32) i32.const 1, i32.const 9, br 0, end: the branch carries only the 9.
	wasm := i32Func(nil,
		block(tI32), i32c(1), i32c(9), br(0), end,
		end)
	if got := callI32(t, wasm, 0); got != 9 {
		t.Fatalf("expected 9, got %d", got)
	}

	// br_if out of a nested block with a value.
	nested := i32Func(nil,
		block(tI32),
		block(tEmpty),
		i32c(3), lget(0), brIf(1), op(0x1A),
		end,
		i32c(4),
		end,
		end)
	if got := callI32(t, nested, 1); got != 3 {
		t.Fatalf("br_if taken: expected 3, got %d", got)
	}
	if got := callI32(t, nested, 0); got != 4 {
		t.Fatalf("br_if not taken: expected 4, got %d", got)
	}
}

func TestBrTable(t *testing.T) {
	// switch(x) { case 0: 100; case 1: 101; default: 102 }
	wasm := i32Func(nil,
		block(tEmpty),
		block(tEmpty),
		block(tEmpty),
		lget(0), brTable(2, 0, 1),
		end,
		i32c(100), op(0x0F),
		end,
		i32c(101), op(0x0F),
		end,
		i32c(102),
		end)
	for arg, want := range map[int32]int32{0: 100, 1: 101, 2: 102, -1: 102, 99: 102} {
		if got := callI32(t, wasm, arg); got != want {
			t.Fatalf("br_table(%d): expected %d, got %d", arg, want, got)
		}
	}
}

func TestLoopWithBrTableBackEdge(t *testing.T) {
	// Count down with br_table targeting the loop (depth 0) or the block (depth 1).
	wasm := i32Func([]local{{1, tI32}},
		block(tEmpty),
		loop(tEmpty),
		lget(1), i32c(1), op(0x6A), lset(1),
		lget(0), i32c(1), op(0x6B), ltee(0),
		op(0x45), brTable(0, 0, 1), // eqz -> 0: loop again, 1: exit
		end,
		end,
		lget(1),
		end)
	if got := callI32(t, wasm, 5); got != 5 {
		t.Fatalf("expected 5 iterations, got %d", got)
	}
}

func TestReturnFromNestedBlocks(t *testing.T) {
	wasm := i32Func(nil,
		block(tEmpty), loop(tEmpty),
		i32c(77), op(0x0F),
		end, end,
		i32c(0),
		end)
	if got := callI32(t, wasm, 0); got != 77 {
		t.Fatalf("expected 77, got %d", got)
	}
}

func TestRecursiveCalls(t *testing.T) {
	// fact(n) = n <= 1 ? 1 : n * fact(n-1)
	fact := body(nil,
		lget(0), i32c(1), op(0x4C),
		ifOp(tI32),
		i32c(1),
		op(0x05),
		lget(0), lget(0), i32c(1), op(0x6B), call(0), op(0x6C),
		end,
		end)
	wasm := wasmModule{
		types:   [][]byte{funcType([]byte{tI32}, []byte{tI32})},
		funcs:   []uint32{0},
		exports: [][]byte{exportFunc("f", 0)},
		codes:   [][]byte{fact},
	}.bytes()
	if got := callI32(t, wasm, 10); got != 3628800 {
		t.Fatalf("fact(10): expected 3628800, got %d", got)
	}
}

func TestCallWithLocalsOfEveryType(t *testing.T) {
	// callee(i32) -> i64 has locals of all four types, which must start at zero.
	callee := body([]local{{1, tI64}, {1, tF32}, {1, tF64}, {1, tI32}},
		lget(1), lget(0), op(0xAC), op(0x7C), // i64 local + extend(arg)
		lget(2), op(0xAF), op(0x7C), // + trunc_u(f32 local)
		lget(3), op(0xB1), op(0x7C), // + trunc_u(f64 local)
		lget(4), op(0xAC), op(0x7C), // + extend(i32 local)
		end)
	wasm := wasmModule{
		types:   [][]byte{funcType([]byte{tI32}, []byte{tI64}), funcType(nil, []byte{tI64})},
		funcs:   []uint32{0, 1},
		exports: [][]byte{exportFunc("callee", 0), exportFunc("f", 1)},
		codes:   [][]byte{callee, body(nil, i32c(41), call(0), i64c(1), op(0x7C), end)},
	}.bytes()
	inst := instantiate(t, wasm, wasminterpreter.DefaultConfig(), nil)
	if res := mustCall(t, inst, "f"); res[0] != wasminterpreter.I64(42) {
		t.Fatalf("expected i64 42, got %v", res)
	}
	if res := mustCall(t, inst, "callee", wasminterpreter.I32(5)); res[0] != wasminterpreter.I64(5) {
		t.Fatalf("expected i64 5, got %v", res)
	}
}

func TestCallStackExhausted(t *testing.T) {
	wasm := singleFunc(nil, nil, nil, false, call(0), end)
	cfg := wasminterpreter.DefaultConfig()
	cfg.MaxCallDepth = 16
	inst := instantiate(t, wasm, cfg, nil)
	_, err := inst.Call("f")
	if !errors.Is(err, wasminterpreter.ErrTrap) || errCode(err) != "call_stack_exhausted" {
		t.Fatalf("expected call_stack_exhausted trap, got %v", err)
	}
}

func TestMaxStackDepth(t *testing.T) {
	wasm := singleFunc(nil, nil, nil, false, i32c(1), i32c(2), i32c(3), i32c(4), i32c(5), op(0x1A, 0x1A, 0x1A, 0x1A, 0x1A), end)
	cfg := wasminterpreter.DefaultConfig()
	cfg.MaxStackDepth = 4
	inst := instantiate(t, wasm, cfg, nil)
	if _, err := inst.Call("f"); errCode(err) != "limit_exceeded" {
		t.Fatalf("expected limit_exceeded, got %v", err)
	}
	cfg.MaxStackDepth = 5
	inst = instantiate(t, wasm, cfg, nil)
	mustCall(t, inst, "f")
}

func TestUnreachableTrap(t *testing.T) {
	inst := instantiate(t, singleFunc(nil, nil, nil, false, op(0x00), end), wasminterpreter.DefaultConfig(), nil)
	_, err := inst.Call("f")
	if !errors.Is(err, wasminterpreter.ErrTrap) || errCode(err) != "unreachable" {
		t.Fatalf("expected unreachable trap, got %v", err)
	}
}

func TestSelectLocalTeeAndGlobals(t *testing.T) {
	// global 0 is a mutable i32 = 42; f(c) stores select(10, global0, c) into global 0 and returns it.
	// Globals of the other three types exercise every constant kind.
	wasm := wasmModule{
		types: [][]byte{funcType([]byte{tI32}, []byte{tI32})},
		funcs: []uint32{0},
		globals: [][]byte{
			cat([]byte{tI32, 0x01}, i32c(42), end),
			cat([]byte{tI64, 0x00}, i64c(-1), end),
			cat([]byte{tF32, 0x00}, f32c(1.5), end),
			cat([]byte{tF64, 0x00}, f64c(2.5), end),
		},
		exports: [][]byte{exportFunc("f", 0)},
		codes: [][]byte{body([]local{{1, tI32}},
			i32c(10), ltee(1), gget(0), lget(0), op(0x1B), gset(0), gget(0),
			end)},
	}.bytes()
	inst := instantiate(t, wasm, wasminterpreter.DefaultConfig(), nil)
	if res := mustCall(t, inst, "f", wasminterpreter.I32(1)); res[0].I32 != 10 {
		t.Fatalf("select true: expected 10, got %v", res)
	}
	fresh := instantiate(t, wasm, wasminterpreter.DefaultConfig(), nil)
	if res := mustCall(t, fresh, "f", wasminterpreter.I32(0)); res[0].I32 != 42 {
		t.Fatalf("select false: expected 42, got %v", res)
	}
}

func TestGlobalInitFromGlobalGet(t *testing.T) {
	// A global initialised from an earlier global via global.get.
	wasm := wasmModule{
		types: [][]byte{funcType(nil, []byte{tI32})},
		funcs: []uint32{0},
		globals: [][]byte{
			cat([]byte{tI32, 0x00}, i32c(7), end),
			cat([]byte{tI32, 0x00}, gget(0), end),
		},
		exports: [][]byte{exportFunc("f", 0)},
		codes:   [][]byte{body(nil, gget(1), end)},
	}.bytes()
	inst := instantiate(t, wasm, wasminterpreter.DefaultConfig(), nil)
	if res := mustCall(t, inst, "f"); res[0].I32 != 7 {
		t.Fatalf("expected 7, got %v", res)
	}
}

func TestInstantiateConstExprErrors(t *testing.T) {
	cases := map[string][]byte{
		"type mismatch":       cat([]byte{tI32, 0x00}, i64c(1), end),
		"unsupported op":      cat([]byte{tI32, 0x00}, i32c(1), i32c(2), op(0x6A), end),
		"global out of range": cat([]byte{tI32, 0x00}, gget(5), end),
	}
	for label, g := range cases {
		t.Run(label, func(t *testing.T) {
			mod, err := wasminterpreter.Compile(wasmModule{globals: [][]byte{g}}.bytes(), wasminterpreter.DefaultConfig())
			if err != nil {
				t.Fatalf("compile failed: %v", err)
			}
			if _, err := wasminterpreter.Instantiate(mod, nil); errCode(err) != "validation_failed" {
				t.Fatalf("expected validation_failed, got %v", err)
			}
		})
	}
}

func TestStartFunction(t *testing.T) {
	// The start function stores 99 at address 0.
	wasm := wasmModule{
		types:   [][]byte{funcType(nil, nil)},
		funcs:   []uint32{0},
		mems:    [][]byte{limits(1, -1)},
		start:   u32p(0),
		codes:   [][]byte{body(nil, i32c(0), i32c(99), mem(0x36, 2, 0), end)},
		data:    [][]byte{cat([]byte{0x00}, i32c(10), end, name("hello"))},
		exports: [][]byte{exportFunc("f", 0)},
	}.bytes()
	inst := instantiate(t, wasm, wasminterpreter.DefaultConfig(), nil)
	m := inst.Memory()
	if m[0] != 99 {
		t.Fatalf("start function did not run: mem[0]=%d", m[0])
	}
	if string(m[10:15]) != "hello" {
		t.Fatalf("expected 'hello' at offset 10, got %q", string(m[10:15]))
	}

	trapping := wasmModule{
		types: [][]byte{funcType(nil, nil)},
		funcs: []uint32{0},
		start: u32p(0),
		codes: [][]byte{body(nil, op(0x00), end)},
	}.bytes()
	mod, err := wasminterpreter.Compile(trapping, wasminterpreter.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wasminterpreter.Instantiate(mod, nil); !errors.Is(err, wasminterpreter.ErrTrap) {
		t.Fatalf("expected start trap, got %v", err)
	}
}

func TestTableAndCallIndirect(t *testing.T) {
	// func 0, 1: () -> i32; func 2: (i32) -> i32 dispatches through the table; func 3 has type 1.
	wasm := wasmModule{
		types:   [][]byte{funcType(nil, []byte{tI32}), funcType([]byte{tI32}, []byte{tI32})},
		funcs:   []uint32{0, 0, 1, 1},
		tables:  [][]byte{cat([]byte{0x70}, limits(4, 4))},
		exports: [][]byte{exportFunc("call", 2)},
		elems:   [][]byte{cat([]byte{0x00}, i32c(0), end, vec(uleb(0), uleb(1), uleb(3)))},
		codes: [][]byte{
			body(nil, i32c(100), end),
			body(nil, i32c(200), end),
			body(nil, lget(0), callInd(0), end),
			body(nil, lget(0), end),
		},
	}.bytes()
	inst := instantiate(t, wasm, wasminterpreter.DefaultConfig(), nil)
	for idx, want := range map[int32]int32{0: 100, 1: 200} {
		if res := mustCall(t, inst, "call", wasminterpreter.I32(idx)); res[0].I32 != want {
			t.Fatalf("call(%d): expected %d, got %v", idx, want, res)
		}
	}
	trapCases := map[int32]string{
		2:  "indirect_call_type_mismatch",
		3:  "undefined_element", // table slot without an element
		5:  "undefined_element", // out of table bounds
		-1: "undefined_element",
	}
	for idx, code := range trapCases {
		_, err := inst.Call("call", wasminterpreter.I32(idx))
		if !errors.Is(err, wasminterpreter.ErrTrap) || errCode(err) != code {
			t.Fatalf("call(%d): expected %s trap, got %v", idx, code, err)
		}
	}
}

func TestCallIndirectToHostFunction(t *testing.T) {
	imports := map[string]map[string]wasminterpreter.HostFunc{
		"env": {"seven": func(*wasminterpreter.Instance, []wasminterpreter.Value) ([]wasminterpreter.Value, error) {
			return []wasminterpreter.Value{wasminterpreter.I32(7)}, nil
		}},
	}
	wasm := wasmModule{
		types:   [][]byte{funcType(nil, []byte{tI32})},
		imports: [][]byte{importFunc("env", "seven", 0)},
		funcs:   []uint32{0},
		tables:  [][]byte{cat([]byte{0x70}, limits(1, -1))},
		exports: [][]byte{exportFunc("f", 1)},
		elems:   [][]byte{cat([]byte{0x00}, i32c(0), end, vec(uleb(0)))},
		codes:   [][]byte{body(nil, i32c(0), callInd(0), end)},
	}.bytes()
	inst := instantiate(t, wasm, wasminterpreter.DefaultConfig(), imports)
	if res := mustCall(t, inst, "f"); res[0].I32 != 7 {
		t.Fatalf("expected 7, got %v", res)
	}
}

func TestCallIndirectWithoutTable(t *testing.T) {
	wasm := wasmModule{
		types:   [][]byte{funcType(nil, nil)},
		funcs:   []uint32{0},
		exports: [][]byte{exportFunc("f", 0)},
		codes:   [][]byte{body(nil, i32c(0), callInd(0), end)},
	}.bytes()
	inst := instantiate(t, wasm, wasminterpreter.DefaultConfig(), nil)
	if _, err := inst.Call("f"); errCode(err) != "undefined_element" {
		t.Fatalf("expected undefined_element, got %v", err)
	}
}

func TestInstantiateSegmentBounds(t *testing.T) {
	cases := map[string]wasmModule{
		"element segment out of table": {
			types:  [][]byte{funcType(nil, nil)},
			funcs:  []uint32{0},
			tables: [][]byte{cat([]byte{0x70}, limits(1, -1))},
			elems:  [][]byte{cat([]byte{0x00}, i32c(1), end, vec(uleb(0)))},
			codes:  [][]byte{body(nil, end)},
		},
		"data segment out of memory": {
			mems: [][]byte{limits(1, -1)},
			data: [][]byte{cat([]byte{0x00}, i32c(65535), end, name("xy"))},
		},
		"negative data offset": {
			mems: [][]byte{limits(1, -1)},
			data: [][]byte{cat([]byte{0x00}, i32c(-1), end, name("x"))},
		},
	}
	for label, m := range cases {
		t.Run(label, func(t *testing.T) {
			mod, err := wasminterpreter.Compile(m.bytes(), wasminterpreter.DefaultConfig())
			if err != nil {
				t.Fatalf("compile failed: %v", err)
			}
			if _, err := wasminterpreter.Instantiate(mod, nil); !errors.Is(err, wasminterpreter.ErrTrap) {
				t.Fatalf("expected trap, got %v", err)
			}
		})
	}

	t.Run("initial memory above MaxMemoryPages", func(t *testing.T) {
		cfg := wasminterpreter.DefaultConfig()
		cfg.MaxMemoryPages = 2
		mod, err := wasminterpreter.Compile(wasmModule{mems: [][]byte{limits(3, -1)}}.bytes(), cfg)
		if err != nil {
			t.Fatalf("compile failed: %v", err)
		}
		if _, err := wasminterpreter.Instantiate(mod, nil); errCode(err) != "limit_exceeded" {
			t.Fatalf("expected limit_exceeded, got %v", err)
		}
	})
}

func TestBlockResultTypes(t *testing.T) {
	cases := []struct {
		bt   byte
		val  []byte
		want wasminterpreter.Value
	}{
		{tI32, i32c(-3), i32v(-3)},
		{tI64, i64c(1 << 40), i64v(1 << 40)},
		{tF32, f32c(1.25), f32v(1.25)},
		{tF64, f64c(-0.5), f64v(-0.5)},
	}
	for _, tc := range cases {
		// block (result T) value br 0 end, and the same through an if/else.
		viaBlock := singleFunc(nil, []byte{tc.bt}, nil, false, block(tc.bt), tc.val, br(0), end, end)
		viaIf := singleFunc(nil, []byte{tc.bt}, nil, false, i32c(0), ifOp(tc.bt), tc.val, op(0x05), tc.val, end, end)
		for _, wasm := range [][]byte{viaBlock, viaIf} {
			res := mustCall(t, instantiate(t, wasm, wasminterpreter.DefaultConfig(), nil), "f")
			if len(res) != 1 || !sameValue(res[0], tc.want) {
				t.Fatalf("block type 0x%02x: expected %v, got %v", tc.bt, tc.want, res)
			}
		}
	}
}

func TestLoopWithResultType(t *testing.T) {
	// loop (result i32): count down, then leave the counter as the loop's result.
	wasm := i32Func(nil,
		loop(tI32),
		lget(0), i32c(1), op(0x6B), ltee(0),
		brIf(0),
		lget(0),
		end,
		end)
	if got := callI32(t, wasm, 4); got != 0 {
		t.Fatalf("expected 0, got %d", got)
	}
}
