package wasminterpreter_test

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"testing"
	"testing/quick"

	"github.com/Trysac/u403-wasm-interpreter"
)

// errCode returns the Code of a *wasminterpreter.Error, or "" when err is not one.
func errCode(err error) string {
	var wasmErr *wasminterpreter.Error
	if errors.As(err, &wasmErr) {
		return wasmErr.Code
	}
	return ""
}

// instantiate compiles and instantiates wasm, failing the test on any error.
func instantiate(t *testing.T, wasm []byte, cfg wasminterpreter.Config, imports map[string]map[string]wasminterpreter.HostFunc) *wasminterpreter.Instance {
	t.Helper()
	mod, err := wasminterpreter.Compile(wasm, cfg)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	inst, err := wasminterpreter.Instantiate(mod, imports)
	if err != nil {
		t.Fatalf("instantiate failed: %v", err)
	}
	return inst
}

// mustCall calls an export and fails the test on error.
func mustCall(t *testing.T, inst *wasminterpreter.Instance, fn string, args ...wasminterpreter.Value) []wasminterpreter.Value {
	t.Helper()
	res, err := inst.Call(fn, args...)
	if err != nil {
		t.Fatalf("call %s failed: %v", fn, err)
	}
	return res
}

// divModule exports "div"(i32, i32) -> i32 computing i32.div_s.
func divModule() []byte {
	return wasmModule{
		types:   [][]byte{funcType([]byte{tI32, tI32}, []byte{tI32})},
		funcs:   []uint32{0},
		exports: [][]byte{exportFunc("div", 0)},
		codes:   [][]byte{body(nil, lget(0), lget(1), op(0x6D), end)},
	}.bytes()
}

func TestAcceptanceCases(t *testing.T) {
	t.Run("invalid magic", func(t *testing.T) {
		invalidMagic := []byte{0x00, 0x61, 0x73, 0x00, 0x01, 0x00, 0x00, 0x00}
		_, err := wasminterpreter.Compile(invalidMagic, wasminterpreter.DefaultConfig())
		if errCode(err) != "invalid_magic" {
			t.Fatalf("expected code 'invalid_magic', got %v", err)
		}
	})

	t.Run("limit exceeded module bytes", func(t *testing.T) {
		cfg := wasminterpreter.Config{MaxModuleBytes: 16777216}
		oversized := make([]byte, 16777217)
		_, err := wasminterpreter.Compile(oversized, cfg)
		if errCode(err) != "limit_exceeded" {
			t.Fatalf("expected code 'limit_exceeded', got %v", err)
		}
	})

	t.Run("i32.div_s overflow MinInt32 by -1", func(t *testing.T) {
		inst := instantiate(t, divModule(), wasminterpreter.DefaultConfig(), nil)
		_, err := inst.Call("div", wasminterpreter.I32(-2147483648), wasminterpreter.I32(-1))
		if !errors.Is(err, wasminterpreter.ErrTrap) {
			t.Fatalf("expected ErrTrap, got %v", err)
		}
		if errCode(err) != "integer_overflow" {
			t.Fatalf("expected code 'integer_overflow', got %v", err)
		}
	})

	t.Run("memory.grow exceeding MaxMemoryPages returns -1", func(t *testing.T) {
		wasm := wasmModule{
			types:   [][]byte{funcType([]byte{tI32}, []byte{tI32})},
			funcs:   []uint32{0},
			mems:    [][]byte{limits(1, -1)},
			exports: [][]byte{exportFunc("grow", 0)},
			codes:   [][]byte{body(nil, lget(0), op(0x40, 0x00), end)},
		}.bytes()
		cfg := wasminterpreter.DefaultConfig()
		cfg.MaxMemoryPages = 256
		inst := instantiate(t, wasm, cfg, nil)
		if len(inst.Memory()) != 65536 {
			t.Fatalf("expected 65536 bytes, got %d", len(inst.Memory()))
		}
		res := mustCall(t, inst, "grow", wasminterpreter.I32(300))
		if len(res) != 1 || res[0].I32 != -1 {
			t.Fatalf("expected -1, got %v", res)
		}
		if len(inst.Memory()) != 65536 {
			t.Fatalf("memory should remain 65536 bytes, got %d", len(inst.Memory()))
		}
	})

	t.Run("fuel 5 with 6 instructions runs out of fuel", func(t *testing.T) {
		wasm := singleFunc(nil, nil, nil, false, op(0x01, 0x01, 0x01, 0x01, 0x01, 0x01), end)
		cfg := wasminterpreter.DefaultConfig()
		cfg.Fuel = 5
		inst := instantiate(t, wasm, cfg, nil)
		_, err := inst.Call("f")
		if !errors.Is(err, wasminterpreter.ErrOutOfFuel) {
			t.Fatalf("expected ErrOutOfFuel, got %v", err)
		}
		if errCode(err) != "out_of_fuel" {
			t.Fatalf("expected code 'out_of_fuel', got %v", err)
		}
		if errors.Is(err, wasminterpreter.ErrTrap) {
			t.Fatalf("out_of_fuel must not be a trap: %v", err)
		}
	})

	t.Run("fuel is deterministic", func(t *testing.T) {
		wasm := wasmModule{
			types:   [][]byte{funcType([]byte{tI32}, []byte{tI32})},
			funcs:   []uint32{0},
			exports: [][]byte{exportFunc("sum", 0)},
			codes:   [][]byte{sumBody()},
		}.bytes()
		cfg := wasminterpreter.DefaultConfig()
		cfg.Fuel = 100000
		var remaining [2]uint64
		for i := range remaining {
			inst := instantiate(t, wasm, cfg, nil)
			res := mustCall(t, inst, "sum", wasminterpreter.I32(20))
			if res[0].I32 != 210 {
				t.Fatalf("expected 210, got %v", res)
			}
			remaining[i] = inst.RemainingFuel()
		}
		if remaining[0] != remaining[1] {
			t.Fatalf("fuel not deterministic: %d vs %d", remaining[0], remaining[1])
		}
		if remaining[0] >= 100000 {
			t.Fatalf("expected fuel to be consumed, remaining %d", remaining[0])
		}
	})

	t.Run("exported add evaluates 20 + 22 to 42", func(t *testing.T) {
		wasm := wasmModule{
			types:   [][]byte{funcType(nil, []byte{tI32})},
			funcs:   []uint32{0},
			exports: [][]byte{exportFunc("add", 0)},
			codes:   [][]byte{body(nil, i32c(20), i32c(22), op(0x6A), end)},
		}.bytes()
		inst := instantiate(t, wasm, wasminterpreter.DefaultConfig(), nil)
		res := mustCall(t, inst, "add")
		if len(res) != 1 || res[0] != wasminterpreter.I32(42) {
			t.Fatalf("expected []Value{I32(42)}, got %v", res)
		}
	})
}

func TestFuelAccounting(t *testing.T) {
	// Three instructions: i32.const, i32.const, i32.add, plus the final end.
	wasm := singleFunc(nil, []byte{tI32}, nil, false, i32c(1), i32c(2), op(0x6A), end)
	cfg := wasminterpreter.DefaultConfig()
	cfg.Fuel = 10
	inst := instantiate(t, wasm, cfg, nil)
	mustCall(t, inst, "f")
	if got := inst.RemainingFuel(); got != 6 {
		t.Fatalf("expected 6 fuel left after 4 instructions, got %d", got)
	}
	inst.SetFuel(2)
	if _, err := inst.Call("f"); !errors.Is(err, wasminterpreter.ErrOutOfFuel) {
		t.Fatalf("expected out of fuel after SetFuel(2), got %v", err)
	}
	inst.SetFuel(4)
	if res := mustCall(t, inst, "f"); res[0].I32 != 3 {
		t.Fatalf("expected 3, got %v", res)
	}
	if inst.RemainingFuel() != 0 {
		t.Fatalf("expected 0 fuel left, got %d", inst.RemainingFuel())
	}

	// Fuel 0 means unmetered.
	unmetered := instantiate(t, wasm, wasminterpreter.DefaultConfig(), nil)
	mustCall(t, unmetered, "f")
	if unmetered.RemainingFuel() != 0 {
		t.Fatalf("unmetered instance should report 0, got %d", unmetered.RemainingFuel())
	}
}

func TestHostImports(t *testing.T) {
	var stdoutBuf bytes.Buffer
	hostImports := map[string]map[string]wasminterpreter.HostFunc{
		"wasi_snapshot_preview1": {
			"fd_write": func(caller *wasminterpreter.Instance, args []wasminterpreter.Value) ([]wasminterpreter.Value, error) {
				stdoutBuf.WriteString(fmt.Sprintf("fd=%d written", args[0].I32))
				return []wasminterpreter.Value{wasminterpreter.I32(0)}, nil
			},
		},
	}
	wasm := wasmModule{
		types:   [][]byte{funcType([]byte{tI32}, []byte{tI32}), funcType(nil, []byte{tI32})},
		imports: [][]byte{importFunc("wasi_snapshot_preview1", "fd_write", 0)},
		funcs:   []uint32{1},
		exports: [][]byte{exportFunc("test", 1), exportFunc("direct", 0)},
		codes:   [][]byte{body(nil, i32c(1), call(0), end)},
	}.bytes()

	inst := instantiate(t, wasm, wasminterpreter.DefaultConfig(), hostImports)
	res := mustCall(t, inst, "test")
	if len(res) != 1 || res[0].I32 != 0 {
		t.Fatalf("expected 0, got %v", res)
	}
	if stdoutBuf.String() != "fd=1 written" {
		t.Fatalf("unexpected stdout buf: %s", stdoutBuf.String())
	}

	// An exported import is callable directly.
	stdoutBuf.Reset()
	mustCall(t, inst, "direct", wasminterpreter.I32(2))
	if stdoutBuf.String() != "fd=2 written" {
		t.Fatalf("unexpected stdout buf: %s", stdoutBuf.String())
	}

	t.Run("host error propagates", func(t *testing.T) {
		boom := errors.New("boom")
		failing := map[string]map[string]wasminterpreter.HostFunc{
			"wasi_snapshot_preview1": {
				"fd_write": func(*wasminterpreter.Instance, []wasminterpreter.Value) ([]wasminterpreter.Value, error) {
					return nil, boom
				},
			},
		}
		inst := instantiate(t, wasm, wasminterpreter.DefaultConfig(), failing)
		if _, err := inst.Call("test"); !errors.Is(err, boom) {
			t.Fatalf("expected host error, got %v", err)
		}
	})

	t.Run("host can read caller memory", func(t *testing.T) {
		var seen string
		imports := map[string]map[string]wasminterpreter.HostFunc{
			"env": {"log": func(caller *wasminterpreter.Instance, args []wasminterpreter.Value) ([]wasminterpreter.Value, error) {
				m := caller.Memory()
				seen = string(m[args[0].I32 : args[0].I32+args[1].I32])
				return nil, nil
			}},
		}
		wasm := wasmModule{
			types:   [][]byte{funcType([]byte{tI32, tI32}, nil), funcType(nil, nil)},
			imports: [][]byte{importFunc("env", "log", 0)},
			funcs:   []uint32{1},
			mems:    [][]byte{limits(1, -1)},
			exports: [][]byte{exportFunc("run", 1)},
			codes:   [][]byte{body(nil, i32c(8), i32c(2), call(0), end)},
			data:    [][]byte{cat([]byte{0x00}, i32c(8), end, name("hi"))},
		}.bytes()
		inst := instantiate(t, wasm, wasminterpreter.DefaultConfig(), imports)
		mustCall(t, inst, "run")
		if seen != "hi" {
			t.Fatalf("expected host to read %q, got %q", "hi", seen)
		}
	})

	t.Run("missing import is a link error", func(t *testing.T) {
		mod, err := wasminterpreter.Compile(wasm, wasminterpreter.DefaultConfig())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := wasminterpreter.Instantiate(mod, nil); errCode(err) != "link_error" {
			t.Fatalf("expected link_error, got %v", err)
		}
	})

	t.Run("non-function import is a link error", func(t *testing.T) {
		wasm := wasmModule{
			imports: [][]byte{cat(name("env"), name("mem"), []byte{0x02}, limits(1, -1))},
		}.bytes()
		mod, err := wasminterpreter.Compile(wasm, wasminterpreter.DefaultConfig())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := wasminterpreter.Instantiate(mod, nil); errCode(err) != "link_error" {
			t.Fatalf("expected link_error, got %v", err)
		}
	})
}

func TestCallArguments(t *testing.T) {
	inst := instantiate(t, divModule(), wasminterpreter.DefaultConfig(), nil)
	if _, err := inst.Call("div", wasminterpreter.I32(1)); errCode(err) != "validation_failed" {
		t.Fatalf("expected validation_failed for arity, got %v", err)
	}
	if _, err := inst.Call("div", wasminterpreter.I32(1), wasminterpreter.I64(1)); errCode(err) != "validation_failed" {
		t.Fatalf("expected validation_failed for type, got %v", err)
	}
	if _, err := inst.Call("nonexistent"); errCode(err) != "export_not_found" {
		t.Fatalf("expected export_not_found, got %v", err)
	}
	if res := mustCall(t, inst, "div", wasminterpreter.I32(-7), wasminterpreter.I32(2)); res[0].I32 != -3 {
		t.Fatalf("expected -3, got %v", res)
	}
}

func TestExportOfNonFunctionIsNotCallable(t *testing.T) {
	wasm := wasmModule{
		mems:    [][]byte{limits(1, -1)},
		exports: [][]byte{cat(name("memory"), []byte{0x02, 0x00})},
	}.bytes()
	inst := instantiate(t, wasm, wasminterpreter.DefaultConfig(), nil)
	if _, err := inst.Call("memory"); errCode(err) != "export_not_found" {
		t.Fatalf("expected export_not_found, got %v", err)
	}
}

func TestPropertyQuick(t *testing.T) {
	// For any a and b where b != 0 and (a, b) != (MinInt32, -1), i32.div_s matches Go's a / b.
	inst := instantiate(t, divModule(), wasminterpreter.DefaultConfig(), nil)
	f := func(a int32, b int32) bool {
		if b == 0 || (a == math.MinInt32 && b == -1) {
			return true
		}
		res, err := inst.Call("div", wasminterpreter.I32(a), wasminterpreter.I32(b))
		return err == nil && len(res) == 1 && res[0].I32 == a/b
	}
	cfg := &quick.Config{MaxCount: 200, Rand: newRand()}
	if err := quick.Check(f, cfg); err != nil {
		t.Fatalf("quick check failed: %v", err)
	}
}

func FuzzCompile(f *testing.F) {
	f.Add([]byte{0x00, 0x61, 0x73, 0x6D, 0x01, 0x00, 0x00, 0x00})
	f.Add([]byte{0x00, 0x61, 0x73, 0x00})
	f.Add([]byte{})
	f.Add([]byte("not a wasm module"))
	f.Add(divModule())
	f.Add(wasmModule{types: [][]byte{funcType(nil, nil)}, globals: [][]byte{cat([]byte{tI32, 0x00}, i32c(1))}}.bytes())

	f.Fuzz(func(t *testing.T, data []byte) {
		cfg := wasminterpreter.DefaultConfig()
		mod, err := wasminterpreter.Compile(data, cfg)
		if err != nil {
			var wasmErr *wasminterpreter.Error
			if !errors.As(err, &wasmErr) {
				t.Fatalf("returned error does not unwrap to *Error: %v", err)
			}
			if wasmErr.Code == "" {
				t.Fatalf("error has empty code")
			}
			return
		}
		_, _ = wasminterpreter.Instantiate(mod, nil)
	})
}

func ExampleCompile() {
	wasm := []byte{
		0x00, 0x61, 0x73, 0x6D, 0x01, 0x00, 0x00, 0x00,
		0x01, 0x05, 0x01, 0x60, 0x00, 0x01, 0x7F,
		0x03, 0x02, 0x01, 0x00,
		0x07, 0x07, 0x01, 0x03, 'a', 'd', 'd', 0x00, 0x00,
		0x0A, 0x09, 0x01, 0x07, 0x00, 0x41, 0x14, 0x41, 0x16, 0x6A, 0x0B,
	}
	mod, err := wasminterpreter.Compile(wasm, wasminterpreter.DefaultConfig())
	if err != nil {
		panic(err)
	}
	inst, err := wasminterpreter.Instantiate(mod, nil)
	if err != nil {
		panic(err)
	}
	res, _ := inst.Call("add")
	fmt.Printf("result: %v\n", res[0])
	// Output:
	// result: i32:42
}
