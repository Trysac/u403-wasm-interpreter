package wasminterpreter_test

import (
	"encoding/binary"
	"math"
)

// This file holds a tiny WebAssembly binary builder used by the tests. Every section and function
// body is length-prefixed by the builder, so hand-written byte literals cannot drift from the
// sizes they declare.

// Value type and block type bytes.
const (
	tI32   byte = 0x7F
	tI64   byte = 0x7E
	tF32   byte = 0x7D
	tF64   byte = 0x7C
	tEmpty byte = 0x40
)

// wasmHeader returns the magic number and version 1.
func wasmHeader() []byte {
	return []byte{0x00, 0x61, 0x73, 0x6D, 0x01, 0x00, 0x00, 0x00}
}

// uleb encodes v as unsigned LEB128.
func uleb(v uint64) []byte {
	var out []byte
	for {
		b := byte(v & 0x7F)
		v >>= 7
		if v != 0 {
			out = append(out, b|0x80)
			continue
		}
		return append(out, b)
	}
}

// sleb encodes v as signed LEB128.
func sleb(v int64) []byte {
	var out []byte
	for {
		b := byte(v & 0x7F)
		v >>= 7
		if (v == 0 && b&0x40 == 0) || (v == -1 && b&0x40 != 0) {
			return append(out, b)
		}
		out = append(out, b|0x80)
	}
}

// cat concatenates byte slices.
func cat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// vec encodes a vector: element count followed by the elements.
func vec(items ...[]byte) []byte {
	return cat(uleb(uint64(len(items))), cat(items...))
}

// name encodes a length-prefixed UTF-8 name.
func name(s string) []byte {
	return cat(uleb(uint64(len(s))), []byte(s))
}

// section encodes a section with its computed payload size.
func section(id byte, payload []byte) []byte {
	return cat([]byte{id}, uleb(uint64(len(payload))), payload)
}

// funcType encodes a function signature.
func funcType(params, results []byte) []byte {
	return cat([]byte{0x60}, uleb(uint64(len(params))), params, uleb(uint64(len(results))), results)
}

// local is one group of locals in a function body.
type local struct {
	n  uint32
	vt byte
}

// body encodes a size-prefixed function body. The code must include its final end (0x0B).
func body(locals []local, code ...[]byte) []byte {
	groups := uleb(uint64(len(locals)))
	for _, l := range locals {
		groups = cat(groups, uleb(uint64(l.n)), []byte{l.vt})
	}
	payload := cat(groups, cat(code...))
	return cat(uleb(uint64(len(payload))), payload)
}

// Instruction helpers.
func op(b ...byte) []byte     { return b }
func i32c(v int32) []byte     { return cat([]byte{0x41}, sleb(int64(v))) }
func i64c(v int64) []byte     { return cat([]byte{0x42}, sleb(v)) }
func lget(i uint32) []byte    { return cat([]byte{0x20}, uleb(uint64(i))) }
func lset(i uint32) []byte    { return cat([]byte{0x21}, uleb(uint64(i))) }
func ltee(i uint32) []byte    { return cat([]byte{0x22}, uleb(uint64(i))) }
func gget(i uint32) []byte    { return cat([]byte{0x23}, uleb(uint64(i))) }
func gset(i uint32) []byte    { return cat([]byte{0x24}, uleb(uint64(i))) }
func call(i uint32) []byte    { return cat([]byte{0x10}, uleb(uint64(i))) }
func br(d uint32) []byte      { return cat([]byte{0x0C}, uleb(uint64(d))) }
func brIf(d uint32) []byte    { return cat([]byte{0x0D}, uleb(uint64(d))) }
func block(bt byte) []byte    { return []byte{0x02, bt} }
func loop(bt byte) []byte     { return []byte{0x03, bt} }
func ifOp(bt byte) []byte     { return []byte{0x04, bt} }
func callInd(t uint32) []byte { return cat([]byte{0x11}, uleb(uint64(t)), []byte{0x00}) }

func f32c(v float32) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, math.Float32bits(v))
	return cat([]byte{0x43}, b)
}

func f64c(v float64) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, math.Float64bits(v))
	return cat([]byte{0x44}, b)
}

// mem encodes a load or store with alignment and offset immediates.
func mem(opcode byte, align, offset uint32) []byte {
	return cat([]byte{opcode}, uleb(uint64(align)), uleb(uint64(offset)))
}

func brTable(def uint32, targets ...uint32) []byte {
	out := cat([]byte{0x0E}, uleb(uint64(len(targets))))
	for _, t := range targets {
		out = cat(out, uleb(uint64(t)))
	}
	return cat(out, uleb(uint64(def)))
}

// end is the end opcode.
var end = []byte{0x0B}

// exportFunc encodes a function export entry.
func exportFunc(n string, idx uint32) []byte {
	return cat(name(n), []byte{0x00}, uleb(uint64(idx)))
}

// importFunc encodes a function import entry.
func importFunc(mod, field string, typeIdx uint32) []byte {
	return cat(name(mod), name(field), []byte{0x00}, uleb(uint64(typeIdx)))
}

// limits encodes table or memory limits; max < 0 means no maximum.
func limits(minPages uint32, maxPages int64) []byte {
	if maxPages < 0 {
		return cat([]byte{0x00}, uleb(uint64(minPages)))
	}
	return cat([]byte{0x01}, uleb(uint64(minPages)), uleb(uint64(maxPages)))
}

// wasmModule describes a module section by section; empty fields are omitted.
type wasmModule struct {
	types   [][]byte
	imports [][]byte
	funcs   []uint32
	tables  [][]byte
	mems    [][]byte
	globals [][]byte
	exports [][]byte
	start   *uint32
	elems   [][]byte
	codes   [][]byte
	data    [][]byte
}

// bytes encodes the module in the canonical section order.
func (m wasmModule) bytes() []byte {
	out := wasmHeader()
	add := func(id byte, items [][]byte) {
		if len(items) > 0 {
			out = cat(out, section(id, vec(items...)))
		}
	}
	add(1, m.types)
	add(2, m.imports)
	if len(m.funcs) > 0 {
		idx := make([][]byte, len(m.funcs))
		for i, f := range m.funcs {
			idx[i] = uleb(uint64(f))
		}
		add(3, idx)
	}
	add(4, m.tables)
	add(5, m.mems)
	add(6, m.globals)
	add(7, m.exports)
	if m.start != nil {
		out = cat(out, section(8, uleb(uint64(*m.start))))
	}
	add(9, m.elems)
	add(10, m.codes)
	add(11, m.data)
	return out
}

// singleFunc builds a module with one function exported as "f" and an optional 1-page memory.
func singleFunc(params, results []byte, locals []local, withMemory bool, code ...[]byte) []byte {
	m := wasmModule{
		types:   [][]byte{funcType(params, results)},
		funcs:   []uint32{0},
		exports: [][]byte{exportFunc("f", 0)},
		codes:   [][]byte{body(locals, code...)},
	}
	if withMemory {
		m.mems = [][]byte{limits(1, -1)}
	}
	return m.bytes()
}

func u32p(v uint32) *uint32 { return &v }
