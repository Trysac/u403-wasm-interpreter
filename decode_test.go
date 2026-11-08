package wasminterpreter_test

import (
	"testing"

	"github.com/Trysac/u403-wasm-interpreter"
)

// withSections appends raw sections to the header.
func withSections(sections ...[]byte) []byte {
	return cat(wasmHeader(), cat(sections...))
}

// codeOnly builds a module with one () -> () function whose body holds the raw bytes given.
func codeOnly(raw ...byte) []byte {
	return withSections(
		section(1, vec(funcType(nil, nil))),
		section(3, vec(uleb(0))),
		section(10, vec(body(nil, raw))),
	)
}

func typeSec(payload ...byte) []byte { return withSections(section(1, payload)) }

func TestMalformedModules(t *testing.T) {
	huge := uleb(0xFFFFFFFF)
	cases := map[string][]byte{
		"truncated version":         {0x00, 0x61, 0x73, 0x6D, 0x01, 0x00},
		"truncated magic":           {0x00, 0x61},
		"section size too long":     withSections([]byte{0x01, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}),
		"missing section size":      withSections([]byte{0x01}),
		"truncated section content": withSections([]byte{0x01, 0x05, 0x01}),
		"unknown section id":        withSections(section(12, nil)),
		"sections out of order":     withSections(section(3, vec()), section(1, vec())),
		"duplicate section":         withSections(section(1, vec()), section(1, vec())),

		"type count":        typeSec(0x80),
		"type form":         typeSec(0x01, 0x61, 0x00, 0x00),
		"type param count":  typeSec(0x01, 0x60),
		"type param":        typeSec(0x01, 0x60, 0x01),
		"type result count": typeSec(0x01, 0x60, 0x00),
		"type result":       typeSec(0x01, 0x60, 0x00, 0x01),
		"type huge params":  withSections(section(1, cat(uleb(1), []byte{0x60}, huge))),
		"type huge results": withSections(section(1, cat(uleb(1), []byte{0x60, 0x00}, huge))),
		"type huge count":   withSections(section(1, huge)),

		"import count":          withSections(section(2, []byte{0x80})),
		"import module name":    withSections(section(2, cat(uleb(1), []byte{0x05, 'a'}))),
		"import field name":     withSections(section(2, cat(uleb(1), name("m"), []byte{0x05}))),
		"import kind":           withSections(section(2, cat(uleb(1), name("m"), name("f")))),
		"import unknown kind":   withSections(section(2, cat(uleb(1), name("m"), name("f"), []byte{0x04, 0x00}))),
		"import func type":      withSections(section(2, cat(uleb(1), name("m"), name("f"), []byte{0x00}))),
		"import table type":     withSections(section(2, cat(uleb(1), name("m"), name("f"), []byte{0x01}))),
		"import table limits":   withSections(section(2, cat(uleb(1), name("m"), name("f"), []byte{0x01, 0x70}))),
		"import mem limits":     withSections(section(2, cat(uleb(1), name("m"), name("f"), []byte{0x02, 0x02, 0x00}))),
		"import global type":    withSections(section(2, cat(uleb(1), name("m"), name("f"), []byte{0x03}))),
		"import global mut":     withSections(section(2, cat(uleb(1), name("m"), name("f"), []byte{0x03, tI32}))),
		"func count":            withSections(section(3, []byte{0x80})),
		"func type index":       withSections(section(3, []byte{0x01})),
		"table count":           withSections(section(4, []byte{0x80})),
		"table elem type":       withSections(section(4, []byte{0x01})),
		"table limits":          withSections(section(4, []byte{0x01, 0x70})),
		"table limits max":      withSections(section(4, []byte{0x01, 0x70, 0x01, 0x00})),
		"mem count":             withSections(section(5, []byte{0x80})),
		"mem limit flag":        withSections(section(5, []byte{0x01, 0x02, 0x00})),
		"mem limits min":        withSections(section(5, []byte{0x01, 0x00})),
		"global count":          withSections(section(6, []byte{0x80})),
		"global type":           withSections(section(6, []byte{0x01})),
		"global mutability":     withSections(section(6, []byte{0x01, tI32})),
		"global expr no end":    withSections(section(6, cat(uleb(1), []byte{tI32, 0x00}, i32c(1)))),
		"global expr bad const": withSections(section(6, cat(uleb(1), []byte{tI32, 0x00, 0x41}))),
		"export count":          withSections(section(7, []byte{0x80})),
		"export name":           withSections(section(7, []byte{0x01, 0x03, 'a'})),
		"export kind":           withSections(section(7, cat(uleb(1), name("f")))),
		"export index":          withSections(section(7, cat(uleb(1), name("f"), []byte{0x00}))),
		"start index":           withSections(section(8, []byte{0x80})),
		"elem count":            withSections(section(9, []byte{0x80})),
		"elem table index":      withSections(section(9, []byte{0x01})),
		"elem offset":           withSections(section(9, []byte{0x01, 0x00})),
		"elem item count":       withSections(section(9, cat(uleb(1), uleb(0), i32c(0), end))),
		"elem item":             withSections(section(9, cat(uleb(1), uleb(0), i32c(0), end, uleb(2), uleb(0)))),
		"elem huge item count":  withSections(section(9, cat(uleb(1), uleb(0), i32c(0), end, huge))),
		"code count":            withSections(section(10, []byte{0x80})),
		"code body size":        withSections(section(10, []byte{0x01})),
		"code truncated body":   withSections(section(10, []byte{0x01, 0x0A, 0x00})),
		"data count":            withSections(section(11, []byte{0x80})),
		"data mem index":        withSections(section(11, []byte{0x01})),
		"data offset":           withSections(section(11, []byte{0x01, 0x00})),
		"data length":           withSections(section(11, cat(uleb(1), uleb(0), i32c(0), end))),
		"data truncated":        withSections(section(11, cat(uleb(1), uleb(0), i32c(0), end, uleb(4), []byte{'a'}))),

		"local group count":      withSections(section(1, vec(funcType(nil, nil))), section(3, vec(uleb(0))), section(10, vec([]byte{0x01, 0x80}))),
		"local count":            withSections(section(1, vec(funcType(nil, nil))), section(3, vec(uleb(0))), section(10, vec([]byte{0x02, 0x01, 0x80}))),
		"local type":             withSections(section(1, vec(funcType(nil, nil))), section(3, vec(uleb(0))), section(10, vec([]byte{0x02, 0x01, 0x01}))),
		"block type":             codeOnly(0x02),
		"invalid block type":     codeOnly(0x02, 0x7B, 0x0B, 0x0B),
		"br label":               codeOnly(0x0C),
		"br_table count":         codeOnly(0x0E),
		"br_table target":        codeOnly(0x0E, 0x02, 0x00),
		"br_table default":       codeOnly(0x0E, 0x01, 0x00),
		"br_table huge count":    codeOnly(cat([]byte{0x0E}, huge)...),
		"call index":             codeOnly(0x10),
		"call_indirect type":     codeOnly(0x11),
		"call_indirect reserved": codeOnly(0x11, 0x00, 0x01, 0x0B),
		"local index":            codeOnly(0x20),
		"global index":           codeOnly(0x23),
		"mem align":              codeOnly(0x28),
		"mem offset":             codeOnly(0x28, 0x02),
		"memory reserved":        codeOnly(0x3F, 0x01, 0x0B),
		"memory reserved eof":    codeOnly(0x40),
		"i32.const":              codeOnly(0x41),
		"i32.const too long":     codeOnly(0x41, 0x80, 0x80, 0x80, 0x80, 0x80, 0x00),
		"i64.const":              codeOnly(0x42),
		"i64.const too long":     codeOnly(0x42, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x00),
		"f32.const":              codeOnly(0x43, 0x00),
		"f64.const":              codeOnly(0x44, 0x00),
		"u32 too long":           codeOnly(0x20, 0x80, 0x80, 0x80, 0x80, 0x80, 0x00),
	}
	for label, wasm := range cases {
		t.Run(label, func(t *testing.T) {
			_, err := wasminterpreter.Compile(wasm, wasminterpreter.DefaultConfig())
			switch errCode(err) {
			case "malformed_section", "invalid_magic", "limit_exceeded":
			default:
				t.Fatalf("expected a typed decode error, got %v", err)
			}
		})
	}
}

func TestTooManyLocals(t *testing.T) {
	wasm := withSections(
		section(1, vec(funcType(nil, nil))),
		section(3, vec(uleb(0))),
		section(10, vec(body([]local{{0xFFFFFFFF, tI32}, {0xFFFFFFFF, tI32}}, end))),
	)
	_, err := wasminterpreter.Compile(wasm, wasminterpreter.DefaultConfig())
	if code := errCode(err); code != "malformed_section" && code != "limit_exceeded" {
		t.Fatalf("expected too many locals to be rejected, got %v", err)
	}
}

func TestValidatorRejections(t *testing.T) {
	cases := map[string]wasmModule{
		"two memories":         {mems: [][]byte{limits(1, -1), limits(1, -1)}},
		"two tables":           {tables: [][]byte{cat([]byte{0x70}, limits(1, -1)), cat([]byte{0x70}, limits(1, -1))}},
		"memory min above max": {mems: [][]byte{limits(2, 1)}},
		"table min above max":  {tables: [][]byte{cat([]byte{0x70}, limits(2, 1))}},
		"func without body":    {types: [][]byte{funcType(nil, nil)}, funcs: []uint32{0}},
		"body without func":    {codes: [][]byte{body(nil, end)}},
	}
	for label, m := range cases {
		t.Run(label, func(t *testing.T) {
			_, err := wasminterpreter.Compile(m.bytes(), wasminterpreter.DefaultConfig())
			if errCode(err) != "validation_failed" {
				t.Fatalf("expected validation_failed, got %v", err)
			}
		})
	}
}

func TestDecodesEveryImportKindAndCustomSections(t *testing.T) {
	wasm := withSections(
		section(0, cat(name("custom"), []byte{1, 2, 3})),
		section(1, vec(funcType([]byte{tI32, tI64, tF32, tF64}, []byte{tI32}))),
		section(2, vec(
			importFunc("env", "f", 0),
			cat(name("env"), name("t"), []byte{0x01, 0x70}, limits(1, 2)),
			cat(name("env"), name("m"), []byte{0x02}, limits(1, -1)),
			cat(name("env"), name("g"), []byte{0x03, tI32, 0x01}),
		)),
		section(0, name("trailing")),
	)
	mod, err := wasminterpreter.Compile(wasm, wasminterpreter.DefaultConfig())
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if len(mod.Imports) != 4 || len(mod.ImportFuncs) != 1 {
		t.Fatalf("expected 4 imports (1 function), got %d (%d)", len(mod.Imports), len(mod.ImportFuncs))
	}
	if g := mod.Imports[3].Desc.Global; g.ValType != wasminterpreter.ValTypeI32 || !g.Mutable {
		t.Fatalf("unexpected global import %+v", g)
	}
	if lim := mod.Imports[1].Desc.Table.Limits; lim.Min != 1 || lim.Max == nil || *lim.Max != 2 {
		t.Fatalf("unexpected table limits %+v", lim)
	}
}

func TestMultiByteLEBImmediates(t *testing.T) {
	// i32.const with a negative multi-byte immediate and i64.const with a large one.
	cases := []struct {
		code []byte
		want wasminterpreter.Value
	}{
		{i32c(-123456), i32v(-123456)},
		{i32c(math32Min()), i32v(math32Min())},
		{i64c(-1 << 62), i64v(-1 << 62)},
		{i64c(1 << 40), i64v(1 << 40)},
	}
	for _, tc := range cases {
		inst := instantiate(t, singleFunc(nil, []byte{resultType(tc.want)}, nil, false, tc.code, end), wasminterpreter.DefaultConfig(), nil)
		if res := mustCall(t, inst, "f"); !sameValue(res[0], tc.want) {
			t.Fatalf("expected %v, got %v", tc.want, res)
		}
	}
}

func math32Min() int32 { return -1 << 31 }
