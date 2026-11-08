package wasminterpreter_test

import (
	"errors"
	"math"
	"testing"

	"github.com/Trysac/u403-wasm-interpreter"
)

type numCase struct {
	name string
	code []byte
	want wasminterpreter.Value
}

// resultType maps a Value to the result type byte of the function that returns it.
func resultType(v wasminterpreter.Value) byte {
	return byte(v.Typ)
}

// sameValue compares two values bit for bit, so NaN payloads and signed zeros matter.
func sameValue(a, b wasminterpreter.Value) bool {
	if a.Typ != b.Typ {
		return false
	}
	switch a.Typ {
	case wasminterpreter.ValTypeI32:
		return a.I32 == b.I32
	case wasminterpreter.ValTypeI64:
		return a.I64 == b.I64
	case wasminterpreter.ValTypeF32:
		return math.Float32bits(a.F32) == math.Float32bits(b.F32)
	case wasminterpreter.ValTypeF64:
		return math.Float64bits(a.F64) == math.Float64bits(b.F64)
	}
	return false
}

func i32v(v int32) wasminterpreter.Value   { return wasminterpreter.I32(v) }
func i64v(v int64) wasminterpreter.Value   { return wasminterpreter.I64(v) }
func f32v(v float32) wasminterpreter.Value { return wasminterpreter.F32(v) }
func f64v(v float64) wasminterpreter.Value { return wasminterpreter.F64(v) }

var (
	nan32     = math.Float32frombits(0x7fc00000)
	nan64     = math.Float64frombits(0x7ff8000000000000)
	negZero32 = float32(math.Copysign(0, -1))
	negZero64 = math.Copysign(0, -1)
)

func b32(a, b int32, opcode byte) []byte    { return cat(i32c(a), i32c(b), op(opcode)) }
func b64(a, b int64, opcode byte) []byte    { return cat(i64c(a), i64c(b), op(opcode)) }
func bf32(a, b float32, opcode byte) []byte { return cat(f32c(a), f32c(b), op(opcode)) }
func bf64(a, b float64, opcode byte) []byte { return cat(f64c(a), f64c(b), op(opcode)) }
func uf32(a float32, opcode byte) []byte    { return cat(f32c(a), op(opcode)) }
func uf64(a float64, opcode byte) []byte    { return cat(f64c(a), op(opcode)) }
func ui32(a int32, opcode byte) []byte      { return cat(i32c(a), op(opcode)) }
func ui64(a int64, opcode byte) []byte      { return cat(i64c(a), op(opcode)) }

func numericCases() []numCase {
	return []numCase{
		// i32 arithmetic and bitwise
		{"i32.add", b32(20, 22, 0x6A), i32v(42)},
		{"i32.add wraps", b32(math.MaxInt32, 1, 0x6A), i32v(math.MinInt32)},
		{"i32.sub", b32(10, 2, 0x6B), i32v(8)},
		{"i32.mul", b32(6, 7, 0x6C), i32v(42)},
		{"i32.div_s", b32(-7, 2, 0x6D), i32v(-3)},
		{"i32.div_u", b32(-1, 2, 0x6E), i32v(math.MaxInt32)},
		{"i32.rem_s", b32(-7, 2, 0x6F), i32v(-1)},
		{"i32.rem_s min by -1", b32(math.MinInt32, -1, 0x6F), i32v(0)},
		{"i32.rem_u", b32(7, 3, 0x70), i32v(1)},
		{"i32.and", b32(0xF0, 0x3C, 0x71), i32v(0x30)},
		{"i32.or", b32(0xF0, 0x3C, 0x72), i32v(0xFC)},
		{"i32.xor", b32(0xF0, 0x3C, 0x73), i32v(0xCC)},
		{"i32.shl masks count", b32(1, 33, 0x74), i32v(2)},
		{"i32.shr_s", b32(-8, 1, 0x75), i32v(-4)},
		{"i32.shr_u", b32(-8, 28, 0x76), i32v(15)},
		{"i32.rotl", b32(math.MinInt32+1, 1, 0x77), i32v(3)},
		{"i32.rotr", b32(3, 1, 0x78), i32v(math.MinInt32 + 1)},
		{"i32.clz", ui32(8, 0x67), i32v(28)},
		{"i32.ctz", ui32(8, 0x68), i32v(3)},
		{"i32.popcnt", ui32(-1, 0x69), i32v(32)},
		{"i32.eqz zero", ui32(0, 0x45), i32v(1)},
		{"i32.eqz nonzero", ui32(5, 0x45), i32v(0)},
		// i32 comparisons
		{"i32.eq", b32(1, 1, 0x46), i32v(1)},
		{"i32.ne", b32(1, 1, 0x47), i32v(0)},
		{"i32.lt_s", b32(-1, 1, 0x48), i32v(1)},
		{"i32.lt_u", b32(-1, 1, 0x49), i32v(0)},
		{"i32.gt_s", b32(-1, 1, 0x4A), i32v(0)},
		{"i32.gt_u", b32(-1, 1, 0x4B), i32v(1)},
		{"i32.le_s", b32(1, 1, 0x4C), i32v(1)},
		{"i32.le_u", b32(2, 1, 0x4D), i32v(0)},
		{"i32.ge_s", b32(-1, 1, 0x4E), i32v(0)},
		{"i32.ge_u", b32(-1, 1, 0x4F), i32v(1)},

		// i64 arithmetic and bitwise
		{"i64.add", b64(100, 50, 0x7C), i64v(150)},
		{"i64.sub", b64(20, 10, 0x7D), i64v(10)},
		{"i64.mul", b64(1<<32, 3, 0x7E), i64v(3 << 32)},
		{"i64.div_s", b64(-20, 3, 0x7F), i64v(-6)},
		{"i64.div_u", b64(-1, 2, 0x80), i64v(math.MaxInt64)},
		{"i64.rem_s", b64(-21, 4, 0x81), i64v(-1)},
		{"i64.rem_s min by -1", b64(math.MinInt64, -1, 0x81), i64v(0)},
		{"i64.rem_u", b64(21, 4, 0x82), i64v(1)},
		{"i64.and", b64(0xF, 3, 0x83), i64v(3)},
		{"i64.or", b64(0xC, 3, 0x84), i64v(0xF)},
		{"i64.xor", b64(0xF, 3, 0x85), i64v(0xC)},
		{"i64.shl masks count", b64(1, 65, 0x86), i64v(2)},
		{"i64.shr_s", b64(-8, 1, 0x87), i64v(-4)},
		{"i64.shr_u", b64(-1, 60, 0x88), i64v(15)},
		{"i64.rotl", b64(math.MinInt64, 1, 0x89), i64v(1)},
		{"i64.rotr", b64(1, 1, 0x8A), i64v(math.MinInt64)},
		{"i64.clz", ui64(8, 0x79), i64v(60)},
		{"i64.ctz", ui64(8, 0x7A), i64v(3)},
		{"i64.popcnt", ui64(-1, 0x7B), i64v(64)},
		{"i64.eqz zero", ui64(0, 0x50), i32v(1)},
		{"i64.eqz nonzero", ui64(10, 0x50), i32v(0)},
		// i64 comparisons
		{"i64.eq", b64(5, 5, 0x51), i32v(1)},
		{"i64.ne", b64(5, 5, 0x52), i32v(0)},
		{"i64.lt_s", b64(-1, 1, 0x53), i32v(1)},
		{"i64.lt_u", b64(-1, 1, 0x54), i32v(0)},
		{"i64.gt_s", b64(-1, 1, 0x55), i32v(0)},
		{"i64.gt_u", b64(-1, 1, 0x56), i32v(1)},
		{"i64.le_s", b64(1, 1, 0x57), i32v(1)},
		{"i64.le_u", b64(2, 1, 0x58), i32v(0)},
		{"i64.ge_s", b64(-1, 1, 0x59), i32v(0)},
		{"i64.ge_u", b64(-1, 1, 0x5A), i32v(1)},

		// f32
		{"f32.abs", uf32(-1, 0x8B), f32v(1)},
		{"f32.neg", uf32(1, 0x8C), f32v(-1)},
		{"f32.ceil", uf32(1.2, 0x8D), f32v(2)},
		{"f32.floor", uf32(1.8, 0x8E), f32v(1)},
		{"f32.trunc", uf32(-1.8, 0x8F), f32v(-1)},
		{"f32.nearest ties to even", uf32(2.5, 0x90), f32v(2)},
		{"f32.sqrt", uf32(4, 0x91), f32v(2)},
		{"f32.sqrt negative is canonical NaN", uf32(-1, 0x91), f32v(nan32)},
		{"f32.add", bf32(1.5, 2.5, 0x92), f32v(4)},
		{"f32.sub", bf32(1.5, 2.5, 0x93), f32v(-1)},
		{"f32.mul", bf32(2, 3, 0x94), f32v(6)},
		{"f32.div", bf32(1, 4, 0x95), f32v(0.25)},
		{"f32.div zero by zero is canonical NaN", bf32(0, 0, 0x95), f32v(nan32)},
		{"f32.min", bf32(1, 2, 0x96), f32v(1)},
		{"f32.min NaN", bf32(nan32, 2, 0x96), f32v(nan32)},
		{"f32.min signed zeros", bf32(0, negZero32, 0x96), f32v(negZero32)},
		{"f32.max", bf32(1, 2, 0x97), f32v(2)},
		{"f32.max NaN", bf32(1, nan32, 0x97), f32v(nan32)},
		{"f32.max signed zeros", bf32(negZero32, 0, 0x97), f32v(0)},
		{"f32.copysign", bf32(1, -2, 0x98), f32v(-1)},
		{"f32.eq", bf32(1, 1, 0x5B), i32v(1)},
		{"f32.ne NaN", bf32(nan32, nan32, 0x5C), i32v(1)},
		{"f32.lt", bf32(1, 2, 0x5D), i32v(1)},
		{"f32.gt", bf32(1, 2, 0x5E), i32v(0)},
		{"f32.le", bf32(2, 2, 0x5F), i32v(1)},
		{"f32.ge", bf32(1, 2, 0x60), i32v(0)},

		// f64
		{"f64.abs", uf64(-1, 0x99), f64v(1)},
		{"f64.neg", uf64(1, 0x9A), f64v(-1)},
		{"f64.ceil", uf64(1.2, 0x9B), f64v(2)},
		{"f64.floor", uf64(1.8, 0x9C), f64v(1)},
		{"f64.trunc", uf64(-1.8, 0x9D), f64v(-1)},
		{"f64.nearest ties to even", uf64(3.5, 0x9E), f64v(4)},
		{"f64.sqrt", uf64(4, 0x9F), f64v(2)},
		{"f64.sqrt negative is canonical NaN", uf64(-1, 0x9F), f64v(nan64)},
		{"f64.add", bf64(1.5, 2.5, 0xA0), f64v(4)},
		{"f64.sub", bf64(1.5, 2.5, 0xA1), f64v(-1)},
		{"f64.mul", bf64(2, 3, 0xA2), f64v(6)},
		{"f64.div", bf64(10, 4, 0xA3), f64v(2.5)},
		{"f64.min", bf64(1, 2, 0xA4), f64v(1)},
		{"f64.min NaN", bf64(nan64, 2, 0xA4), f64v(nan64)},
		{"f64.min signed zeros", bf64(0, negZero64, 0xA4), f64v(negZero64)},
		{"f64.max", bf64(1, 2, 0xA5), f64v(2)},
		{"f64.max NaN", bf64(1, nan64, 0xA5), f64v(nan64)},
		{"f64.max signed zeros", bf64(negZero64, 0, 0xA5), f64v(0)},
		{"f64.copysign", bf64(1, -2, 0xA6), f64v(-1)},
		{"f64.eq", bf64(1, 1, 0x61), i32v(1)},
		{"f64.ne", bf64(1, 1, 0x62), i32v(0)},
		{"f64.lt", bf64(1, 2, 0x63), i32v(1)},
		{"f64.gt", bf64(1, 2, 0x64), i32v(0)},
		{"f64.le", bf64(2, 2, 0x65), i32v(1)},
		{"f64.ge", bf64(1, 2, 0x66), i32v(0)},

		// conversions
		{"i32.wrap_i64", ui64(0x100000002, 0xA7), i32v(2)},
		{"i32.trunc_f32_s", uf32(-1.5, 0xA8), i32v(-1)},
		{"i32.trunc_f32_u", uf32(3.7, 0xA9), i32v(3)},
		{"i32.trunc_f64_s", uf64(-2.9, 0xAA), i32v(-2)},
		{"i32.trunc_f64_u", uf64(4e9, 0xAB), i32v(-294967296)},
		{"i64.extend_i32_s", ui32(-1, 0xAC), i64v(-1)},
		{"i64.extend_i32_u", ui32(-1, 0xAD), i64v(4294967295)},
		{"i64.trunc_f32_s", uf32(-3.5, 0xAE), i64v(-3)},
		{"i64.trunc_f32_u", uf32(5.5, 0xAF), i64v(5)},
		{"i64.trunc_f64_s", uf64(-1e10, 0xB0), i64v(-10000000000)},
		{"i64.trunc_f64_u", uf64(1e19, 0xB1), i64v(-8446744073709551616)},
		{"f32.convert_i32_s", ui32(-5, 0xB2), f32v(-5)},
		{"f32.convert_i32_u", ui32(-1, 0xB3), f32v(4294967296)},
		{"f32.convert_i64_s", ui64(-5, 0xB4), f32v(-5)},
		{"f32.convert_i64_u", ui64(-1, 0xB5), f32v(18446744073709551616)},
		{"f32.demote_f64", uf64(1.5, 0xB6), f32v(1.5)},
		{"f32.demote_f64 NaN", uf64(nan64, 0xB6), f32v(nan32)},
		{"f64.convert_i32_s", ui32(-5, 0xB7), f64v(-5)},
		{"f64.convert_i32_u", ui32(-1, 0xB8), f64v(4294967295)},
		{"f64.convert_i64_s", ui64(-5, 0xB9), f64v(-5)},
		{"f64.convert_i64_u", ui64(-1, 0xBA), f64v(18446744073709551616)},
		{"f64.promote_f32", uf32(1.5, 0xBB), f64v(1.5)},
		{"f64.promote_f32 NaN", uf32(nan32, 0xBB), f64v(nan64)},
		{"i32.reinterpret_f32", uf32(1, 0xBC), i32v(0x3F800000)},
		{"i64.reinterpret_f64", uf64(1, 0xBD), i64v(0x3FF0000000000000)},
		{"f32.reinterpret_i32", ui32(0x3F800000, 0xBE), f32v(1)},
		{"f64.reinterpret_i64", ui64(0x3FF0000000000000, 0xBF), f64v(1)},
	}
}

func TestNumericInstructions(t *testing.T) {
	for _, tc := range numericCases() {
		t.Run(tc.name, func(t *testing.T) {
			wasm := singleFunc(nil, []byte{resultType(tc.want)}, nil, false, tc.code, end)
			inst := instantiate(t, wasm, wasminterpreter.DefaultConfig(), nil)
			res := mustCall(t, inst, "f")
			if len(res) != 1 || !sameValue(res[0], tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, res)
			}
		})
	}
}

func TestNumericTraps(t *testing.T) {
	cases := []struct {
		name   string
		code   []byte
		result byte
		want   string // expected code; "" accepts any trap
	}{
		{"i32.div_s by zero", b32(1, 0, 0x6D), tI32, "integer_divide_by_zero"},
		{"i32.div_s overflow", b32(math.MinInt32, -1, 0x6D), tI32, "integer_overflow"},
		{"i32.div_u by zero", b32(1, 0, 0x6E), tI32, "integer_divide_by_zero"},
		{"i32.rem_s by zero", b32(1, 0, 0x6F), tI32, "integer_divide_by_zero"},
		{"i32.rem_u by zero", b32(1, 0, 0x70), tI32, "integer_divide_by_zero"},
		{"i64.div_s by zero", b64(1, 0, 0x7F), tI64, "integer_divide_by_zero"},
		{"i64.div_s overflow", b64(math.MinInt64, -1, 0x7F), tI64, "integer_overflow"},
		{"i64.div_u by zero", b64(1, 0, 0x80), tI64, "integer_divide_by_zero"},
		{"i64.rem_s by zero", b64(1, 0, 0x81), tI64, "integer_divide_by_zero"},
		{"i64.rem_u by zero", b64(1, 0, 0x82), tI64, "integer_divide_by_zero"},
		{"i32.trunc_f32_s NaN", uf32(nan32, 0xA8), tI32, "invalid_conversion"},
		{"i32.trunc_f32_s range", uf32(3e9, 0xA8), tI32, ""},
		{"i32.trunc_f32_u NaN", uf32(nan32, 0xA9), tI32, "invalid_conversion"},
		{"i32.trunc_f32_u range", uf32(-1, 0xA9), tI32, ""},
		{"i32.trunc_f64_s NaN", uf64(nan64, 0xAA), tI32, "invalid_conversion"},
		{"i32.trunc_f64_s range", uf64(-3e9, 0xAA), tI32, ""},
		{"i32.trunc_f64_u NaN", uf64(nan64, 0xAB), tI32, "invalid_conversion"},
		{"i32.trunc_f64_u range", uf64(5e9, 0xAB), tI32, ""},
		{"i64.trunc_f32_s NaN", uf32(nan32, 0xAE), tI64, "invalid_conversion"},
		{"i64.trunc_f32_s range", uf32(1e19, 0xAE), tI64, ""},
		{"i64.trunc_f32_u NaN", uf32(nan32, 0xAF), tI64, "invalid_conversion"},
		{"i64.trunc_f32_u range", uf32(-2, 0xAF), tI64, ""},
		{"i64.trunc_f64_s NaN", uf64(nan64, 0xB0), tI64, "invalid_conversion"},
		{"i64.trunc_f64_s range", uf64(1e19, 0xB0), tI64, ""},
		{"i64.trunc_f64_u NaN", uf64(nan64, 0xB1), tI64, "invalid_conversion"},
		{"i64.trunc_f64_u range", uf64(2e19, 0xB1), tI64, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wasm := singleFunc(nil, []byte{tc.result}, nil, false, tc.code, end)
			inst := instantiate(t, wasm, wasminterpreter.DefaultConfig(), nil)
			_, err := inst.Call("f")
			if !errors.Is(err, wasminterpreter.ErrTrap) {
				t.Fatalf("expected trap, got %v", err)
			}
			if tc.want != "" && errCode(err) != tc.want {
				t.Fatalf("expected code %s, got %v", tc.want, err)
			}
		})
	}
}

// TestStackUnderflowIsAnError checks that operators running on an empty operand stack return an
// error instead of panicking: the interpreter must never crash the host.
func TestStackUnderflowIsAnError(t *testing.T) {
	opcodes := []byte{
		0x1A, 0x1B, 0x21, 0x22, 0x24, 0x04, 0x0D, 0x0E, 0x28, 0x36, 0x40,
		0x45, 0x46, 0x50, 0x51, 0x5B, 0x61, 0x67, 0x68, 0x69, 0x6A, 0x79, 0x7A, 0x7B, 0x7C,
		0x8B, 0x91, 0x92, 0x99, 0x9F, 0xA0, 0xA7, 0xA8, 0xB0, 0xB6, 0xBB, 0xBC, 0xBF,
	}
	immediates := map[byte][]byte{
		0x21: {0x00}, 0x22: {0x00}, 0x24: {0x00}, 0x04: {tEmpty}, 0x0D: {0x00},
		0x0E: {0x00, 0x00}, 0x28: {0x02, 0x00}, 0x36: {0x02, 0x00}, 0x40: {0x00},
	}
	for _, opcode := range opcodes {
		code := cat([]byte{opcode}, immediates[opcode])
		if opcode == 0x04 {
			code = cat(code, end)
		}
		wasm := wasmModule{
			types:   [][]byte{funcType(nil, nil)},
			funcs:   []uint32{0},
			mems:    [][]byte{limits(1, -1)},
			globals: [][]byte{cat([]byte{tI32, 0x01}, i32c(0), end)},
			exports: [][]byte{exportFunc("f", 0)},
			codes:   [][]byte{body([]local{{1, tI32}}, code, end)},
		}.bytes()
		mod, err := wasminterpreter.Compile(wasm, wasminterpreter.DefaultConfig())
		if err != nil {
			continue // rejected by the validator: also acceptable
		}
		inst, err := wasminterpreter.Instantiate(mod, nil)
		if err != nil {
			t.Fatalf("opcode 0x%02x: instantiate failed: %v", opcode, err)
		}
		if _, err := inst.Call("f"); errCode(err) != "validation_failed" {
			t.Fatalf("opcode 0x%02x: expected validation_failed, got %v", opcode, err)
		}
	}
}

func TestUnknownOpcodeIsAnError(t *testing.T) {
	wasm := singleFunc(nil, nil, nil, false, op(0xFF), end)
	mod, err := wasminterpreter.Compile(wasm, wasminterpreter.DefaultConfig())
	if err != nil {
		return // rejected at compile time: acceptable
	}
	inst, err := wasminterpreter.Instantiate(mod, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := inst.Call("f"); errCode(err) == "" {
		t.Fatalf("expected a typed error, got %v", err)
	}
}

func TestValueAndTypeStrings(t *testing.T) {
	cases := map[string]string{
		wasminterpreter.I32(-1).String():       "i32:-1",
		wasminterpreter.I64(5).String():        "i64:5",
		wasminterpreter.F32(1.5).String():      "f32:1.5",
		wasminterpreter.F64(2.5).String():      "f64:2.5",
		wasminterpreter.Value{}.String():       "unknown",
		wasminterpreter.ValTypeI32.String():    "i32",
		wasminterpreter.ValTypeI64.String():    "i64",
		wasminterpreter.ValTypeF32.String():    "f32",
		wasminterpreter.ValTypeF64.String():    "f64",
		wasminterpreter.ValType(0x01).String(): "unknown(0x01)",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("expected %q, got %q", want, got)
		}
	}
}

func TestErrorFormatting(t *testing.T) {
	inner := errors.New("inner")
	e := &wasminterpreter.Error{Code: "link_error", Message: "msg", Err: inner}
	if e.Error() != "wasm [link_error]: msg: inner" {
		t.Fatalf("unexpected message %q", e.Error())
	}
	if !errors.Is(e, inner) {
		t.Fatal("expected Unwrap to expose the wrapped error")
	}
	plain := &wasminterpreter.Error{Code: "malformed_section", Message: "bad"}
	if plain.Error() != "wasm [malformed_section]: bad" {
		t.Fatalf("unexpected message %q", plain.Error())
	}
	if errors.Is(plain, wasminterpreter.ErrTrap) || errors.Is(plain, wasminterpreter.ErrOutOfFuel) {
		t.Fatal("a decode error must not match the trap or fuel sentinels")
	}
	for _, code := range []string{"unreachable", "integer_overflow", "integer_divide_by_zero", "invalid_conversion",
		"out_of_bounds_memory", "undefined_element", "indirect_call_type_mismatch", "call_stack_exhausted"} {
		if !errors.Is(&wasminterpreter.Error{Code: code}, wasminterpreter.ErrTrap) {
			t.Errorf("code %s should match ErrTrap", code)
		}
	}
}

func TestFuncTypeEqual(t *testing.T) {
	a := wasminterpreter.FuncType{Params: []wasminterpreter.ValType{wasminterpreter.ValTypeI32}, Results: []wasminterpreter.ValType{wasminterpreter.ValTypeI64}}
	cases := []struct {
		b    wasminterpreter.FuncType
		want bool
	}{
		{a, true},
		{wasminterpreter.FuncType{}, false},
		{wasminterpreter.FuncType{Params: []wasminterpreter.ValType{wasminterpreter.ValTypeF32}, Results: a.Results}, false},
		{wasminterpreter.FuncType{Params: a.Params, Results: []wasminterpreter.ValType{wasminterpreter.ValTypeF64}}, false},
	}
	for i, tc := range cases {
		if got := a.Equal(tc.b); got != tc.want {
			t.Errorf("case %d: expected %v, got %v", i, tc.want, got)
		}
	}
}

func TestDefaultAndZeroConfig(t *testing.T) {
	d := wasminterpreter.DefaultConfig()
	if d.MaxMemoryPages != 256 || d.MaxStackDepth != 65536 || d.MaxCallDepth != 1024 || d.MaxModuleBytes != 16777216 || d.Fuel != 0 {
		t.Fatalf("unexpected defaults: %+v", d)
	}
	// A zero Config is sanitised to the defaults.
	mod, err := wasminterpreter.Compile(divModule(), wasminterpreter.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if mod.Config != d {
		t.Fatalf("expected zero config to become defaults, got %+v", mod.Config)
	}
}
