package wasminterpreter_test

import (
	"errors"
	"testing"

	"github.com/Trysac/u403-wasm-interpreter"
)

func TestLinearMemoryLoadStore(t *testing.T) {
	cases := []struct {
		name  string
		store []byte // address and value pushed, then the store
		load  []byte // address pushed, then the load
		want  wasminterpreter.Value
	}{
		{"i32", cat(i32c(4), i32c(1337), mem(0x36, 2, 0)), cat(i32c(4), mem(0x28, 2, 0)), i32v(1337)},
		{"i64", cat(i32c(8), i64c(-5), mem(0x37, 3, 0)), cat(i32c(8), mem(0x29, 3, 0)), i64v(-5)},
		{"f32", cat(i32c(0), f32c(1.5), mem(0x38, 2, 0)), cat(i32c(0), mem(0x2A, 2, 0)), f32v(1.5)},
		{"f64", cat(i32c(0), f64c(-2.25), mem(0x39, 3, 0)), cat(i32c(0), mem(0x2B, 3, 0)), f64v(-2.25)},
		{"i32 store8 load8_s", cat(i32c(0), i32c(0xFF), mem(0x3A, 0, 0)), cat(i32c(0), mem(0x2C, 0, 0)), i32v(-1)},
		{"i32 store8 load8_u", cat(i32c(0), i32c(0x1FF), mem(0x3A, 0, 0)), cat(i32c(0), mem(0x2D, 0, 0)), i32v(255)},
		{"i32 store16 load16_s", cat(i32c(0), i32c(0x8000), mem(0x3B, 1, 0)), cat(i32c(0), mem(0x2E, 1, 0)), i32v(-32768)},
		{"i32 store16 load16_u", cat(i32c(0), i32c(0x18000), mem(0x3B, 1, 0)), cat(i32c(0), mem(0x2F, 1, 0)), i32v(32768)},
		{"i64 store8 load8_s", cat(i32c(0), i64c(0x80), mem(0x3C, 0, 0)), cat(i32c(0), mem(0x30, 0, 0)), i64v(-128)},
		{"i64 store8 load8_u", cat(i32c(0), i64c(0x80), mem(0x3C, 0, 0)), cat(i32c(0), mem(0x31, 0, 0)), i64v(128)},
		{"i64 store16 load16_s", cat(i32c(0), i64c(0xFFFF), mem(0x3D, 1, 0)), cat(i32c(0), mem(0x32, 1, 0)), i64v(-1)},
		{"i64 store16 load16_u", cat(i32c(0), i64c(0xFFFF), mem(0x3D, 1, 0)), cat(i32c(0), mem(0x33, 1, 0)), i64v(65535)},
		{"i64 store32 load32_s", cat(i32c(0), i64c(0xFFFFFFFF), mem(0x3E, 2, 0)), cat(i32c(0), mem(0x34, 2, 0)), i64v(-1)},
		{"i64 store32 load32_u", cat(i32c(0), i64c(0xFFFFFFFF), mem(0x3E, 2, 0)), cat(i32c(0), mem(0x35, 2, 0)), i64v(4294967295)},
		{"offset immediate", cat(i32c(0), i32c(77), mem(0x36, 2, 100)), cat(i32c(100), mem(0x28, 2, 0)), i32v(77)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wasm := singleFunc(nil, []byte{resultType(tc.want)}, nil, true, tc.store, tc.load, end)
			inst := instantiate(t, wasm, wasminterpreter.DefaultConfig(), nil)
			res := mustCall(t, inst, "f")
			if len(res) != 1 || !sameValue(res[0], tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, res)
			}
		})
	}
}

func TestLittleEndianLayout(t *testing.T) {
	wasm := singleFunc(nil, nil, nil, true, i32c(0), i32c(0x04030201), mem(0x36, 2, 0), end)
	inst := instantiate(t, wasm, wasminterpreter.DefaultConfig(), nil)
	mustCall(t, inst, "f")
	m := inst.Memory()
	for i, want := range []byte{1, 2, 3, 4} {
		if m[i] != want {
			t.Fatalf("byte %d: expected %d, got %d", i, want, m[i])
		}
	}
}

func TestMemoryOutOfBounds(t *testing.T) {
	cases := map[string][]byte{
		"i32.load at end":     cat(i32c(65533), mem(0x28, 2, 0), op(0x1A)),
		"i64.load offset":     cat(i32c(0), mem(0x29, 3, 65530), op(0x1A)),
		"load8 negative addr": cat(i32c(-1), mem(0x2D, 0, 0), op(0x1A)),
		"i32.store at end":    cat(i32c(65535), i32c(1), mem(0x36, 2, 0)),
		"i64.store32 at end":  cat(i32c(65535), i64c(1), mem(0x3E, 2, 0)),
		"f64.store offset":    cat(i32c(0), f64c(1), mem(0x39, 3, 65536)),
	}
	for label, code := range cases {
		t.Run(label, func(t *testing.T) {
			inst := instantiate(t, singleFunc(nil, nil, nil, true, code, end), wasminterpreter.DefaultConfig(), nil)
			_, err := inst.Call("f")
			if !errors.Is(err, wasminterpreter.ErrTrap) || errCode(err) != "out_of_bounds_memory" {
				t.Fatalf("expected out_of_bounds_memory trap, got %v", err)
			}
		})
	}
	t.Run("no memory", func(t *testing.T) {
		inst := instantiate(t, singleFunc(nil, nil, nil, false, i32c(0), mem(0x28, 2, 0), op(0x1A), end), wasminterpreter.DefaultConfig(), nil)
		if _, err := inst.Call("f"); errCode(err) != "out_of_bounds_memory" {
			t.Fatalf("expected out_of_bounds_memory, got %v", err)
		}
	})
}

func TestMemorySizeAndGrow(t *testing.T) {
	// f(delta) = memory.grow(delta) * 1000 + memory.size
	growAndSize := func(maxPages int64) []byte {
		return wasmModule{
			types:   [][]byte{funcType([]byte{tI32}, []byte{tI32})},
			funcs:   []uint32{0},
			mems:    [][]byte{limits(1, maxPages)},
			exports: [][]byte{exportFunc("f", 0)},
			codes:   [][]byte{body(nil, lget(0), op(0x40, 0x00), i32c(1000), op(0x6C), op(0x3F, 0x00), op(0x6A), end)},
		}.bytes()
	}

	inst := instantiate(t, growAndSize(-1), wasminterpreter.DefaultConfig(), nil)
	if got := mustCall(t, inst, "f", wasminterpreter.I32(2))[0].I32; got != 1003 {
		t.Fatalf("grow by 2 from 1 page: expected 1003, got %d", got)
	}
	if len(inst.Memory()) != 3*65536 {
		t.Fatalf("expected 3 pages, got %d bytes", len(inst.Memory()))
	}
	if got := mustCall(t, inst, "f", wasminterpreter.I32(0))[0].I32; got != 3003 {
		t.Fatalf("grow by 0: expected 3003, got %d", got)
	}

	// The module's declared maximum caps growth below MaxMemoryPages.
	capped := instantiate(t, growAndSize(2), wasminterpreter.DefaultConfig(), nil)
	if got := mustCall(t, capped, "f", wasminterpreter.I32(2))[0].I32; got != -1000+1 {
		t.Fatalf("grow past declared max: expected -999, got %d", got)
	}
	if got := mustCall(t, capped, "f", wasminterpreter.I32(1))[0].I32; got != 1002 {
		t.Fatalf("grow to declared max: expected 1002, got %d", got)
	}

	// Data written before a grow survives it.
	keep := wasmModule{
		types:   [][]byte{funcType(nil, []byte{tI32})},
		funcs:   []uint32{0},
		mems:    [][]byte{limits(1, -1)},
		exports: [][]byte{exportFunc("f", 0)},
		codes:   [][]byte{body(nil, i32c(1), op(0x40, 0x00), op(0x1A), i32c(10), mem(0x2D, 0, 0), end)},
		data:    [][]byte{cat([]byte{0x00}, i32c(10), end, name("\x2A"))},
	}.bytes()
	if got := mustCall(t, instantiate(t, keep, wasminterpreter.DefaultConfig(), nil), "f")[0].I32; got != 42 {
		t.Fatalf("expected data preserved across grow, got %d", got)
	}
}
