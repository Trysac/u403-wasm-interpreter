package wasminterpreter

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"math/bits"
)

// Sentinel errors for errors.Is checking.
var (
	ErrTrap      = errors.New("wasm: trap")
	ErrOutOfFuel = errors.New("wasm: out of fuel")
)

// Error represents typed errors returned by wasminterpreter.
type Error struct {
	Code    string
	Message string
	Err     error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("wasm [%s]: %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("wasm [%s]: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error {
	if e.Code == "out_of_fuel" {
		return ErrOutOfFuel
	}
	switch e.Code {
	case "unreachable", "integer_overflow", "integer_divide_by_zero",
		"invalid_conversion", "out_of_bounds_memory", "undefined_element",
		"indirect_call_type_mismatch", "call_stack_exhausted":
		return ErrTrap
	}
	return e.Err
}

func newError(code string, msg string) *Error {
	return &Error{Code: code, Message: msg}
}

func newTrap(code string, msg string) *Error {
	return &Error{Code: code, Message: msg, Err: ErrTrap}
}

// ValType represents Wasm value types.
type ValType byte

const (
	ValTypeI32 ValType = 0x7F
	ValTypeI64 ValType = 0x7E
	ValTypeF32 ValType = 0x7D
	ValTypeF64 ValType = 0x7C
)

func (vt ValType) String() string {
	switch vt {
	case ValTypeI32:
		return "i32"
	case ValTypeI64:
		return "i64"
	case ValTypeF32:
		return "f32"
	case ValTypeF64:
		return "f64"
	default:
		return fmt.Sprintf("unknown(0x%02x)", byte(vt))
	}
}

// Value is a tagged union holding a single WebAssembly value.
type Value struct {
	Typ ValType
	I32 int32
	I64 int64
	F32 float32
	F64 float64
}

func I32(v int32) Value   { return Value{Typ: ValTypeI32, I32: v} }
func I64(v int64) Value   { return Value{Typ: ValTypeI64, I64: v} }
func F32(v float32) Value { return Value{Typ: ValTypeF32, F32: v} }
func F64(v float64) Value { return Value{Typ: ValTypeF64, F64: v} }

func (v Value) String() string {
	switch v.Typ {
	case ValTypeI32:
		return fmt.Sprintf("i32:%d", v.I32)
	case ValTypeI64:
		return fmt.Sprintf("i64:%d", v.I64)
	case ValTypeF32:
		return fmt.Sprintf("f32:%v", v.F32)
	case ValTypeF64:
		return fmt.Sprintf("f64:%v", v.F64)
	default:
		return "unknown"
	}
}

// Config specifies execution limits and settings for compiling and running modules.
type Config struct {
	MaxMemoryPages uint32
	MaxStackDepth  int
	MaxCallDepth   int
	MaxModuleBytes int64
	Fuel           uint64
}

// DefaultConfig returns safe baseline configuration limits.
func DefaultConfig() Config {
	return Config{
		MaxMemoryPages: 256,
		MaxStackDepth:  65536,
		MaxCallDepth:   1024,
		MaxModuleBytes: 16777216,
		Fuel:           0,
	}
}

func sanitizeConfig(cfg Config) Config {
	if cfg.MaxMemoryPages == 0 {
		cfg.MaxMemoryPages = 256
	}
	if cfg.MaxStackDepth <= 0 {
		cfg.MaxStackDepth = 65536
	}
	if cfg.MaxCallDepth <= 0 {
		cfg.MaxCallDepth = 1024
	}
	if cfg.MaxModuleBytes <= 0 {
		cfg.MaxModuleBytes = 16777216
	}
	return cfg
}

// FuncType describes a function signature.
type FuncType struct {
	Params  []ValType
	Results []ValType
}

func (ft FuncType) Equal(other FuncType) bool {
	if len(ft.Params) != len(other.Params) || len(ft.Results) != len(other.Results) {
		return false
	}
	for i, p := range ft.Params {
		if p != other.Params[i] {
			return false
		}
	}
	for i, r := range ft.Results {
		if r != other.Results[i] {
			return false
		}
	}
	return true
}

// Limits represents min and optional max limits for tables and memories.
type Limits struct {
	Min uint32
	Max *uint32
}

// TableType specifies a table's element type and limits.
type TableType struct {
	ElemType byte
	Limits   Limits
}

// MemType specifies memory limits.
type MemType struct {
	Limits Limits
}

// GlobalType specifies global value type and mutability.
type GlobalType struct {
	ValType ValType
	Mutable bool
}

// Import describes an imported entity.
type Import struct {
	Module string
	Name   string
	Desc   ImportDesc
}

type ImportKind byte

const (
	ImportKindFunc   ImportKind = 0x00
	ImportKindTable  ImportKind = 0x01
	ImportKindMem    ImportKind = 0x02
	ImportKindGlobal ImportKind = 0x03
)

type ImportDesc struct {
	Kind    ImportKind
	TypeIdx uint32
	Table   TableType
	Mem     MemType
	Global  GlobalType
}

// Export describes an exported entity.
type Export struct {
	Name string
	Kind byte
	Idx  uint32
}

// Instruction opcodes and arguments.
type Instr struct {
	Op        byte
	SubOp     byte
	U32       uint32
	U64       uint64
	I32       int32
	I64       int64
	F32       float32
	F64       float64
	BlockType int64 // blockTypeEmpty (-64): no result; -1..-4: one i32/i64/f32/f64 result; >=0: func type idx
	Targets   []uint32
	Default   uint32
	Align     uint32
	Offset    uint32
}

// FunctionBody holds decoded function locals and instructions.
type FunctionBody struct {
	Locals []ValType
	Code   []Instr
}

// GlobalDecl defines an internal global variable.
type GlobalDecl struct {
	Type GlobalType
	Init []Instr
}

// ElementSegment defines table initialization elements.
type ElementSegment struct {
	TableIdx uint32
	Offset   []Instr
	Init     []uint32
}

// DataSegment defines linear memory initialization bytes.
type DataSegment struct {
	MemIdx uint32
	Offset []Instr
	Data   []byte
}

// Module represents a validated, compiled WebAssembly MVP module.
type Module struct {
	Config      Config
	Types       []FuncType
	Imports     []Import
	ImportFuncs []uint32
	Funcs       []uint32 // indices into Types for internal functions
	Tables      []TableType
	Memories    []MemType
	Globals     []GlobalDecl
	Exports     []Export
	Start       *uint32
	Elements    []ElementSegment
	Bodies      []FunctionBody
	Data        []DataSegment
}

// HostFunc is a Go function callable from WebAssembly.
type HostFunc func(caller *Instance, args []Value) ([]Value, error)

// Instance represents an instantiated WebAssembly module with its state.
type Instance struct {
	module   *Module
	cfg      Config
	fuel     uint64
	hasFuel  bool
	memory   []byte
	table    []uint32
	hasTable bool
	globals  []Value
	hostFns  map[uint32]HostFunc
}

// Memory returns the raw linear memory of the instance.
func (inst *Instance) Memory() []byte {
	return inst.memory
}

// RemainingFuel returns remaining execution fuel.
func (inst *Instance) RemainingFuel() uint64 {
	return inst.fuel
}

// SetFuel sets the remaining fuel.
func (inst *Instance) SetFuel(f uint64) {
	inst.fuel = f
	inst.hasFuel = true
}

// Compile decodes and validates a WebAssembly binary module.
func Compile(wasm []byte, cfg Config) (*Module, error) {
	cfg = sanitizeConfig(cfg)
	if int64(len(wasm)) > cfg.MaxModuleBytes {
		return nil, newError("limit_exceeded", fmt.Sprintf("module size %d exceeds limit %d", len(wasm), cfg.MaxModuleBytes))
	}

	mod, err := decodeModule(wasm, cfg)
	if err != nil {
		var wasmErr *Error
		if !errors.As(err, &wasmErr) {
			// Low-level reader errors (such as io.ErrUnexpectedEOF) still get a documented code.
			return nil, &Error{Code: "malformed_section", Message: "unexpected end of module", Err: err}
		}
		return nil, err
	}

	if err := validateModule(mod); err != nil {
		return nil, err
	}

	return mod, nil
}

// Instantiate creates an Instance from a compiled Module and imports.
func Instantiate(mod *Module, imports map[string]map[string]HostFunc) (*Instance, error) {
	inst := &Instance{
		module:  mod,
		cfg:     mod.Config,
		fuel:    mod.Config.Fuel,
		hasFuel: mod.Config.Fuel > 0,
		hostFns: make(map[uint32]HostFunc),
	}

	// Resolve imports
	var funcIdx uint32
	for _, imp := range mod.Imports {
		switch imp.Desc.Kind {
		case ImportKindFunc:
			modMap := imports[imp.Module]
			hf, ok := modMap[imp.Name]
			if !ok {
				return nil, newError("link_error", fmt.Sprintf("unknown import function %s.%s", imp.Module, imp.Name))
			}
			inst.hostFns[funcIdx] = hf
			funcIdx++
		default:
			return nil, newError("link_error", fmt.Sprintf("import kind %d not supported for host imports", imp.Desc.Kind))
		}
	}

	// Setup Memory
	if len(mod.Memories) > 0 {
		memType := mod.Memories[0]
		if memType.Limits.Min > mod.Config.MaxMemoryPages {
			return nil, newError("limit_exceeded", "initial memory exceeds MaxMemoryPages")
		}
		inst.memory = make([]byte, int(memType.Limits.Min)*65536)
	}

	// Setup Table
	if len(mod.Tables) > 0 {
		tblType := mod.Tables[0]
		inst.table = make([]uint32, tblType.Limits.Min)
		for i := range inst.table {
			inst.table[i] = 0xFFFFFFFF // sentinel for undefined
		}
		inst.hasTable = true
	}

	// Initialize Globals
	inst.globals = make([]Value, len(mod.Globals))
	for i, g := range mod.Globals {
		v, err := inst.evalConstExpr(g.Init, g.Type.ValType)
		if err != nil {
			return nil, err
		}
		inst.globals[i] = v
	}

	// Initialize Element Segments
	for _, elem := range mod.Elements {
		offVal, err := inst.evalConstExpr(elem.Offset, ValTypeI32)
		if err != nil {
			return nil, err
		}
		off := int(offVal.I32)
		if off < 0 || off+len(elem.Init) > len(inst.table) {
			return nil, newTrap("out_of_bounds_memory", "element segment out of table bounds")
		}
		for i, idx := range elem.Init {
			inst.table[off+i] = idx
		}
	}

	// Initialize Data Segments
	for _, d := range mod.Data {
		offVal, err := inst.evalConstExpr(d.Offset, ValTypeI32)
		if err != nil {
			return nil, err
		}
		off := int(offVal.I32)
		if off < 0 || off+len(d.Data) > len(inst.memory) {
			return nil, newTrap("out_of_bounds_memory", "data segment out of memory bounds")
		}
		copy(inst.memory[off:], d.Data)
	}

	// Start function if defined
	if mod.Start != nil {
		if _, err := inst.callFuncIdx(*mod.Start, nil); err != nil {
			return nil, err
		}
	}

	return inst, nil
}

// Call executes an exported function by name.
func (inst *Instance) Call(funcName string, args ...Value) ([]Value, error) {
	var exportIdx *uint32
	for _, exp := range inst.module.Exports {
		if exp.Name == funcName && exp.Kind == 0x00 {
			idx := exp.Idx
			exportIdx = &idx
			break
		}
	}
	if exportIdx == nil {
		return nil, newError("export_not_found", fmt.Sprintf("export function '%s' not found", funcName))
	}
	return inst.callFuncIdx(*exportIdx, args)
}

func (inst *Instance) evalConstExpr(instrs []Instr, expectedType ValType) (Value, error) {
	var v Value
	for _, in := range instrs {
		switch in.Op {
		case 0x41: // i32.const
			v = I32(in.I32)
		case 0x42: // i64.const
			v = I64(in.I64)
		case 0x43: // f32.const
			v = F32(in.F32)
		case 0x44: // f64.const
			v = F64(in.F64)
		case 0x23: // global.get
			if int(in.U32) >= len(inst.globals) {
				return Value{}, newError("validation_failed", "global index out of bounds in const expr")
			}
			v = inst.globals[in.U32]
		case 0x0B: // end
			// finished
		default:
			return Value{}, newError("validation_failed", fmt.Sprintf("unsupported const expr op 0x%02x", in.Op))
		}
	}
	if v.Typ != expectedType {
		return Value{}, newError("validation_failed", fmt.Sprintf("const expr type mismatch: got %v, expected %v", v.Typ, expectedType))
	}
	return v, nil
}

// Call frames and control frames
type controlFrame struct {
	op          byte
	blockType   int64
	labelTarget int
	stackHeight int
	resultTypes []ValType
}

type callFrame struct {
	funcIdx   uint32
	locals    []Value
	body      *FunctionBody
	pc        int
	ctrlStack []controlFrame
}

// blockTypeEmpty is the decoded block type of a block, loop or if without results.
const blockTypeEmpty = -0x40

// blockResults returns the result types of a decoded block type.
func (inst *Instance) blockResults(bt int64) []ValType {
	switch {
	case bt >= 0:
		if bt < int64(len(inst.module.Types)) {
			return inst.module.Types[bt].Results
		}
		return nil
	case bt == blockTypeEmpty:
		return nil
	default:
		// -1..-4 are the signed LEB readings of 0x7F (i32) .. 0x7C (f64).
		return []ValType{ValType(0x80 + bt)}
	}
}

func (inst *Instance) getFuncType(idx uint32) (*FuncType, error) {
	numImports := uint32(len(inst.module.ImportFuncs))
	if idx < numImports {
		typeIdx := inst.module.ImportFuncs[idx]
		if int(typeIdx) >= len(inst.module.Types) {
			return nil, newError("validation_failed", "import type index invalid")
		}
		return &inst.module.Types[typeIdx], nil
	}
	internalIdx := idx - numImports
	if int(internalIdx) >= len(inst.module.Funcs) {
		return nil, newError("validation_failed", "function index invalid")
	}
	typeIdx := inst.module.Funcs[internalIdx]
	if int(typeIdx) >= len(inst.module.Types) {
		return nil, newError("validation_failed", "internal type index invalid")
	}
	return &inst.module.Types[typeIdx], nil
}

func (inst *Instance) callFuncIdx(idx uint32, args []Value) ([]Value, error) {
	numImports := uint32(len(inst.module.ImportFuncs))
	if idx < numImports {
		hf, ok := inst.hostFns[idx]
		if !ok {
			return nil, newError("link_error", fmt.Sprintf("unbound host function idx %d", idx))
		}
		return hf(inst, args)
	}

	ft, err := inst.getFuncType(idx)
	if err != nil {
		return nil, err
	}
	if len(args) != len(ft.Params) {
		return nil, newError("validation_failed", fmt.Sprintf("expected %d arguments, got %d", len(ft.Params), len(args)))
	}
	for i, p := range ft.Params {
		if args[i].Typ != p {
			return nil, newError("validation_failed", fmt.Sprintf("argument %d type mismatch: expected %v, got %v", i, p, args[i].Typ))
		}
	}

	internalIdx := idx - numImports
	if int(internalIdx) >= len(inst.module.Bodies) {
		return nil, newError("validation_failed", "function body missing")
	}
	body := &inst.module.Bodies[internalIdx]

	// Initialize explicit stack and call stack
	valStack := make([]Value, 0, 64)
	callStack := make([]callFrame, 0, 16)

	initLocals := make([]Value, len(ft.Params)+len(body.Locals))
	copy(initLocals, args)
	for i, locType := range body.Locals {
		idx := len(ft.Params) + i
		switch locType {
		case ValTypeI32:
			initLocals[idx] = I32(0)
		case ValTypeI64:
			initLocals[idx] = I64(0)
		case ValTypeF32:
			initLocals[idx] = F32(0)
		case ValTypeF64:
			initLocals[idx] = F64(0)
		}
	}

	ctrlStack := []controlFrame{
		{
			op:          0x00, // block
			labelTarget: len(body.Code),
			stackHeight: 0,
			resultTypes: ft.Results,
		},
	}

	callStack = append(callStack, callFrame{
		funcIdx:   idx,
		locals:    initLocals,
		body:      body,
		pc:        0,
		ctrlStack: ctrlStack,
	})

	for len(callStack) > 0 {
		frame := &callStack[len(callStack)-1]
		if frame.pc >= len(frame.body.Code) {
			// Frame finished
			callStack = callStack[:len(callStack)-1]
			continue
		}

		if inst.hasFuel {
			if inst.fuel == 0 {
				return nil, newError("out_of_fuel", "fuel exhausted")
			}
			inst.fuel--
		}

		in := frame.body.Code[frame.pc]
		frame.pc++

		switch in.Op {
		case 0x00: // unreachable
			return nil, newTrap("unreachable", "unreachable executed")
		case 0x01: // nop
			// no-op
		case 0x02, 0x03: // block (0x02), loop (0x03)
			resTypes := inst.blockResults(in.BlockType)
			target := int(in.U32) // for block: end offset; for loop: loop start offset
			if in.Op == 0x03 {
				target = frame.pc - 1 // loop re-enters at loop header
			}
			frame.ctrlStack = append(frame.ctrlStack, controlFrame{
				op:          in.Op,
				blockType:   in.BlockType,
				labelTarget: target,
				stackHeight: len(valStack),
				resultTypes: resTypes,
			})

		case 0x04: // if
			if len(valStack) == 0 {
				return nil, newError("validation_failed", "stack underflow for if")
			}
			c := valStack[len(valStack)-1]
			valStack = valStack[:len(valStack)-1]
			if c.Typ != ValTypeI32 {
				return nil, newError("validation_failed", "if condition must be i32")
			}
			resTypes := inst.blockResults(in.BlockType)
			frame.ctrlStack = append(frame.ctrlStack, controlFrame{
				op:          0x04,
				blockType:   in.BlockType,
				labelTarget: int(in.U32), // target is end
				stackHeight: len(valStack),
				resultTypes: resTypes,
			})
			if c.I32 == 0 {
				// jump to else or end
				frame.pc = int(in.Offset)
			}

		case 0x05: // else
			if len(frame.ctrlStack) == 0 {
				return nil, newError("validation_failed", "else without matching control block")
			}
			ctrl := frame.ctrlStack[len(frame.ctrlStack)-1]
			// jump to end of if
			frame.pc = ctrl.labelTarget

		case 0x0B: // end
			if len(frame.ctrlStack) == 0 {
				return nil, newError("validation_failed", "unmatched end")
			}
			ctrl := frame.ctrlStack[len(frame.ctrlStack)-1]
			frame.ctrlStack = frame.ctrlStack[:len(frame.ctrlStack)-1]
			if len(frame.ctrlStack) == 0 {
				// Return from function call
				callStack = callStack[:len(callStack)-1]
			}
			_ = ctrl

		case 0x0C: // br
			relDepth := int(in.U32)
			if relDepth >= len(frame.ctrlStack) {
				return nil, newError("validation_failed", "branch depth out of range")
			}
			targetCtrl := frame.ctrlStack[len(frame.ctrlStack)-1-relDepth]
			valStack, err = unwindBranch(valStack, targetCtrl)
			if err != nil {
				return nil, err
			}
			frame.pc = targetCtrl.labelTarget
			frame.ctrlStack = truncateForBranch(frame.ctrlStack, relDepth)

		case 0x0D: // br_if
			if len(valStack) == 0 {
				return nil, newError("validation_failed", "stack underflow for br_if")
			}
			c := valStack[len(valStack)-1]
			valStack = valStack[:len(valStack)-1]
			if c.Typ != ValTypeI32 {
				return nil, newError("validation_failed", "br_if condition must be i32")
			}
			if c.I32 != 0 {
				relDepth := int(in.U32)
				if relDepth >= len(frame.ctrlStack) {
					return nil, newError("validation_failed", "branch depth out of range")
				}
				targetCtrl := frame.ctrlStack[len(frame.ctrlStack)-1-relDepth]
				valStack, err = unwindBranch(valStack, targetCtrl)
				if err != nil {
					return nil, err
				}
				frame.pc = targetCtrl.labelTarget
				frame.ctrlStack = truncateForBranch(frame.ctrlStack, relDepth)
			}

		case 0x0E: // br_table
			if len(valStack) == 0 {
				return nil, newError("validation_failed", "stack underflow for br_table")
			}
			idxVal := valStack[len(valStack)-1]
			valStack = valStack[:len(valStack)-1]
			if idxVal.Typ != ValTypeI32 {
				return nil, newError("validation_failed", "br_table index must be i32")
			}
			i := uint32(idxVal.I32)
			depth := in.Default
			if i < uint32(len(in.Targets)) {
				depth = in.Targets[i]
			}
			relDepth := int(depth)
			if relDepth >= len(frame.ctrlStack) {
				return nil, newError("validation_failed", "branch depth out of range")
			}
			targetCtrl := frame.ctrlStack[len(frame.ctrlStack)-1-relDepth]
			valStack, err = unwindBranch(valStack, targetCtrl)
			if err != nil {
				return nil, err
			}
			frame.pc = targetCtrl.labelTarget
			frame.ctrlStack = truncateForBranch(frame.ctrlStack, relDepth)

		case 0x0F: // return
			targetCtrl := frame.ctrlStack[0]
			valStack, err = unwindBranch(valStack, targetCtrl)
			if err != nil {
				return nil, err
			}
			callStack = callStack[:len(callStack)-1]

		case 0x10: // call
			calleeIdx := in.U32
			if len(callStack) >= inst.cfg.MaxCallDepth {
				return nil, newTrap("call_stack_exhausted", "maximum call stack depth exceeded")
			}
			calleeType, err := inst.getFuncType(calleeIdx)
			if err != nil {
				return nil, err
			}
			if len(valStack) < len(calleeType.Params) {
				return nil, newError("validation_failed", "stack underflow for call")
			}
			argsStart := len(valStack) - len(calleeType.Params)
			calleeArgs := make([]Value, len(calleeType.Params))
			copy(calleeArgs, valStack[argsStart:])
			valStack = valStack[:argsStart]

			if calleeIdx < numImports {
				hf, ok := inst.hostFns[calleeIdx]
				if !ok {
					return nil, newError("link_error", fmt.Sprintf("unbound host func idx %d", calleeIdx))
				}
				res, err := hf(inst, calleeArgs)
				if err != nil {
					return nil, err
				}
				valStack = append(valStack, res...)
			} else {
				internalIdx := calleeIdx - numImports
				calleeBody := &inst.module.Bodies[internalIdx]
				locals := make([]Value, len(calleeType.Params)+len(calleeBody.Locals))
				copy(locals, calleeArgs)
				for i, locType := range calleeBody.Locals {
					idx := len(calleeType.Params) + i
					switch locType {
					case ValTypeI32:
						locals[idx] = I32(0)
					case ValTypeI64:
						locals[idx] = I64(0)
					case ValTypeF32:
						locals[idx] = F32(0)
					case ValTypeF64:
						locals[idx] = F64(0)
					}
				}
				newCtrl := []controlFrame{
					{
						op:          0x00,
						labelTarget: len(calleeBody.Code),
						stackHeight: len(valStack),
						resultTypes: calleeType.Results,
					},
				}
				callStack = append(callStack, callFrame{
					funcIdx:   calleeIdx,
					locals:    locals,
					body:      calleeBody,
					pc:        0,
					ctrlStack: newCtrl,
				})
			}

		case 0x11: // call_indirect
			if !inst.hasTable {
				return nil, newTrap("undefined_element", "no table defined for indirect call")
			}
			if len(valStack) == 0 {
				return nil, newError("validation_failed", "stack underflow for call_indirect")
			}
			tblElem := valStack[len(valStack)-1]
			valStack = valStack[:len(valStack)-1]
			if tblElem.Typ != ValTypeI32 {
				return nil, newError("validation_failed", "table index must be i32")
			}
			tblIdx := int(tblElem.I32)
			if tblIdx < 0 || tblIdx >= len(inst.table) {
				return nil, newTrap("undefined_element", "table index out of bounds")
			}
			calleeIdx := inst.table[tblIdx]
			if calleeIdx == 0xFFFFFFFF {
				return nil, newTrap("undefined_element", "uninitialized table element")
			}
			expectedTypeIdx := in.U32
			if int(expectedTypeIdx) >= len(inst.module.Types) {
				return nil, newError("validation_failed", "type index out of bounds in call_indirect")
			}
			expectedType := inst.module.Types[expectedTypeIdx]
			calleeType, err := inst.getFuncType(calleeIdx)
			if err != nil {
				return nil, err
			}
			if !calleeType.Equal(expectedType) {
				return nil, newTrap("indirect_call_type_mismatch", "indirect call signature mismatch")
			}
			if len(callStack) >= inst.cfg.MaxCallDepth {
				return nil, newTrap("call_stack_exhausted", "maximum call stack depth exceeded")
			}
			if len(valStack) < len(calleeType.Params) {
				return nil, newError("validation_failed", "stack underflow for call_indirect")
			}
			argsStart := len(valStack) - len(calleeType.Params)
			calleeArgs := make([]Value, len(calleeType.Params))
			copy(calleeArgs, valStack[argsStart:])
			valStack = valStack[:argsStart]

			if calleeIdx < numImports {
				hf, ok := inst.hostFns[calleeIdx]
				if !ok {
					return nil, newError("link_error", fmt.Sprintf("unbound host func idx %d", calleeIdx))
				}
				res, err := hf(inst, calleeArgs)
				if err != nil {
					return nil, err
				}
				valStack = append(valStack, res...)
			} else {
				internalIdx := calleeIdx - numImports
				calleeBody := &inst.module.Bodies[internalIdx]
				locals := make([]Value, len(calleeType.Params)+len(calleeBody.Locals))
				copy(locals, calleeArgs)
				for i, locType := range calleeBody.Locals {
					idx := len(calleeType.Params) + i
					switch locType {
					case ValTypeI32:
						locals[idx] = I32(0)
					case ValTypeI64:
						locals[idx] = I64(0)
					case ValTypeF32:
						locals[idx] = F32(0)
					case ValTypeF64:
						locals[idx] = F64(0)
					}
				}
				newCtrl := []controlFrame{
					{
						op:          0x00,
						labelTarget: len(calleeBody.Code),
						stackHeight: len(valStack),
						resultTypes: calleeType.Results,
					},
				}
				callStack = append(callStack, callFrame{
					funcIdx:   calleeIdx,
					locals:    locals,
					body:      calleeBody,
					pc:        0,
					ctrlStack: newCtrl,
				})
			}

		case 0x1A: // drop
			if len(valStack) == 0 {
				return nil, newError("validation_failed", "stack underflow for drop")
			}
			valStack = valStack[:len(valStack)-1]

		case 0x1B: // select
			if len(valStack) < 3 {
				return nil, newError("validation_failed", "stack underflow for select")
			}
			c := valStack[len(valStack)-1]
			v2 := valStack[len(valStack)-2]
			v1 := valStack[len(valStack)-3]
			valStack = valStack[:len(valStack)-3]
			if c.Typ != ValTypeI32 || v1.Typ != v2.Typ {
				return nil, newError("validation_failed", "type mismatch in select")
			}
			if c.I32 != 0 {
				valStack = append(valStack, v1)
			} else {
				valStack = append(valStack, v2)
			}

		case 0x20: // local.get
			if int(in.U32) >= len(frame.locals) {
				return nil, newError("validation_failed", "local.get out of bounds")
			}
			valStack = append(valStack, frame.locals[in.U32])

		case 0x21: // local.set
			if len(valStack) == 0 {
				return nil, newError("validation_failed", "stack underflow for local.set")
			}
			v := valStack[len(valStack)-1]
			valStack = valStack[:len(valStack)-1]
			if int(in.U32) >= len(frame.locals) {
				return nil, newError("validation_failed", "local.set out of bounds")
			}
			frame.locals[in.U32] = v

		case 0x22: // local.tee
			if len(valStack) == 0 {
				return nil, newError("validation_failed", "stack underflow for local.tee")
			}
			v := valStack[len(valStack)-1]
			if int(in.U32) >= len(frame.locals) {
				return nil, newError("validation_failed", "local.tee out of bounds")
			}
			frame.locals[in.U32] = v

		case 0x23: // global.get
			if int(in.U32) >= len(inst.globals) {
				return nil, newError("validation_failed", "global.get index out of bounds")
			}
			valStack = append(valStack, inst.globals[in.U32])

		case 0x24: // global.set
			if len(valStack) == 0 {
				return nil, newError("validation_failed", "stack underflow for global.set")
			}
			v := valStack[len(valStack)-1]
			valStack = valStack[:len(valStack)-1]
			if int(in.U32) >= len(inst.globals) {
				return nil, newError("validation_failed", "global.set index out of bounds")
			}
			inst.globals[in.U32] = v

		// Memory Load Instructions (0x28 .. 0x35)
		case 0x28, 0x29, 0x2A, 0x2B, 0x2C, 0x2D, 0x2E, 0x2F, 0x30, 0x31, 0x32, 0x33, 0x34, 0x35:
			if len(valStack) == 0 {
				return nil, newError("validation_failed", "stack underflow for load")
			}
			baseVal := valStack[len(valStack)-1]
			valStack = valStack[:len(valStack)-1]
			if baseVal.Typ != ValTypeI32 {
				return nil, newError("validation_failed", "load address must be i32")
			}
			ea := uint64(uint32(baseVal.I32)) + uint64(in.Offset)
			res, err := inst.executeLoad(in.Op, ea)
			if err != nil {
				return nil, err
			}
			valStack = append(valStack, res)

		// Memory Store Instructions (0x36 .. 0x3E)
		case 0x36, 0x37, 0x38, 0x39, 0x3A, 0x3B, 0x3C, 0x3D, 0x3E:
			if len(valStack) < 2 {
				return nil, newError("validation_failed", "stack underflow for store")
			}
			v := valStack[len(valStack)-1]
			baseVal := valStack[len(valStack)-2]
			valStack = valStack[:len(valStack)-2]
			if baseVal.Typ != ValTypeI32 {
				return nil, newError("validation_failed", "store address must be i32")
			}
			ea := uint64(uint32(baseVal.I32)) + uint64(in.Offset)
			if err := inst.executeStore(in.Op, ea, v); err != nil {
				return nil, err
			}

		case 0x3F: // memory.size
			sizePages := int32(len(inst.memory) / 65536)
			valStack = append(valStack, I32(sizePages))

		case 0x40: // memory.grow
			if len(valStack) == 0 {
				return nil, newError("validation_failed", "stack underflow for memory.grow")
			}
			deltaVal := valStack[len(valStack)-1]
			valStack = valStack[:len(valStack)-1]
			if deltaVal.Typ != ValTypeI32 {
				return nil, newError("validation_failed", "memory.grow delta must be i32")
			}
			curPages := uint32(len(inst.memory) / 65536)
			delta := uint32(deltaVal.I32)
			if uint64(curPages)+uint64(delta) > uint64(inst.cfg.MaxMemoryPages) ||
				(len(inst.module.Memories) > 0 && inst.module.Memories[0].Limits.Max != nil && uint64(curPages)+uint64(delta) > uint64(*inst.module.Memories[0].Limits.Max)) {
				valStack = append(valStack, I32(-1))
			} else {
				newMem := make([]byte, int(curPages+delta)*65536)
				copy(newMem, inst.memory)
				inst.memory = newMem
				valStack = append(valStack, I32(int32(curPages)))
			}

		case 0x41: // i32.const
			valStack = append(valStack, I32(in.I32))
		case 0x42: // i64.const
			valStack = append(valStack, I64(in.I64))
		case 0x43: // f32.const
			valStack = append(valStack, F32(in.F32))
		case 0x44: // f64.const
			valStack = append(valStack, F64(in.F64))

		default:
			// Numeric and conversion instructions
			resStack, err := executeNumeric(in.Op, valStack)
			if err != nil {
				return nil, err
			}
			valStack = resStack
		}

		if len(valStack) > inst.cfg.MaxStackDepth {
			return nil, newError("limit_exceeded", "operand stack depth exceeded MaxStackDepth")
		}
	}

	return valStack, nil
}

// truncateForBranch drops the control frames a branch of relative depth relDepth leaves. A block
// or if frame stays, because its end instruction (the branch target) pops it; a loop frame is
// dropped too, because the branch re-executes the loop instruction, which pushes it again.
func truncateForBranch(ctrlStack []controlFrame, relDepth int) []controlFrame {
	keep := len(ctrlStack) - relDepth
	if ctrlStack[keep-1].op == 0x03 {
		keep--
	}
	return ctrlStack[:keep]
}

// unwindBranch resets the operand stack to the target label's height plus the label's arity: the
// block results, or nothing for a loop (MVP loops take no parameters).
func unwindBranch(valStack []Value, target controlFrame) ([]Value, error) {
	numRes := len(target.resultTypes)
	if target.op == 0x03 {
		numRes = 0
	}
	if len(valStack) < numRes {
		return nil, newError("validation_failed", "stack underflow when unwinding for branch")
	}
	res := make([]Value, numRes)
	copy(res, valStack[len(valStack)-numRes:])
	valStack = valStack[:target.stackHeight]
	valStack = append(valStack, res...)
	return valStack, nil
}

func (inst *Instance) executeLoad(op byte, ea uint64) (Value, error) {
	size := 0
	switch op {
	case 0x28: // i32.load
		size = 4
	case 0x29: // i64.load
		size = 8
	case 0x2A: // f32.load
		size = 4
	case 0x2B: // f64.load
		size = 8
	case 0x2C, 0x2D: // i32.load8_s, i32.load8_u
		size = 1
	case 0x2E, 0x2F: // i32.load16_s, i32.load16_u
		size = 2
	case 0x30, 0x31: // i64.load8_s, i64.load8_u
		size = 1
	case 0x32, 0x33: // i64.load16_s, i64.load16_u
		size = 2
	case 0x34, 0x35: // i64.load32_s, i64.load32_u
		size = 4
	}
	if ea+uint64(size) > uint64(len(inst.memory)) {
		return Value{}, newTrap("out_of_bounds_memory", "memory access out of bounds")
	}
	off := int(ea)
	switch op {
	case 0x28:
		return I32(int32(binary.LittleEndian.Uint32(inst.memory[off : off+4]))), nil
	case 0x29:
		return I64(int64(binary.LittleEndian.Uint64(inst.memory[off : off+8]))), nil
	case 0x2A:
		u := binary.LittleEndian.Uint32(inst.memory[off : off+4])
		return F32(math.Float32frombits(u)), nil
	case 0x2B:
		u := binary.LittleEndian.Uint64(inst.memory[off : off+8])
		return F64(math.Float64frombits(u)), nil
	case 0x2C: // i32.load8_s
		return I32(int32(int8(inst.memory[off]))), nil
	case 0x2D: // i32.load8_u
		return I32(int32(uint8(inst.memory[off]))), nil
	case 0x2E: // i32.load16_s
		return I32(int32(int16(binary.LittleEndian.Uint16(inst.memory[off : off+2])))), nil
	case 0x2F: // i32.load16_u
		return I32(int32(binary.LittleEndian.Uint16(inst.memory[off : off+2]))), nil
	case 0x30: // i64.load8_s
		return I64(int64(int8(inst.memory[off]))), nil
	case 0x31: // i64.load8_u
		return I64(int64(uint8(inst.memory[off]))), nil
	case 0x32: // i64.load16_s
		return I64(int64(int16(binary.LittleEndian.Uint16(inst.memory[off : off+2])))), nil
	case 0x33: // i64.load16_u
		return I64(int64(binary.LittleEndian.Uint16(inst.memory[off : off+2]))), nil
	case 0x34: // i64.load32_s
		return I64(int64(int32(binary.LittleEndian.Uint32(inst.memory[off : off+4])))), nil
	case 0x35: // i64.load32_u
		return I64(int64(binary.LittleEndian.Uint32(inst.memory[off : off+4]))), nil
	}
	return Value{}, newError("validation_failed", fmt.Sprintf("unknown load op 0x%02x", op))
}

func (inst *Instance) executeStore(op byte, ea uint64, val Value) error {
	size := 0
	switch op {
	case 0x36: // i32.store
		size = 4
	case 0x37: // i64.store
		size = 8
	case 0x38: // f32.store
		size = 4
	case 0x39: // f64.store
		size = 8
	case 0x3A, 0x3C: // i32.store8, i64.store8
		size = 1
	case 0x3B, 0x3D: // i32.store16, i64.store16
		size = 2
	case 0x3E: // i64.store32
		size = 4
	}
	if ea+uint64(size) > uint64(len(inst.memory)) {
		return newTrap("out_of_bounds_memory", "memory access out of bounds")
	}
	off := int(ea)
	switch op {
	case 0x36:
		binary.LittleEndian.PutUint32(inst.memory[off:off+4], uint32(val.I32))
	case 0x37:
		binary.LittleEndian.PutUint64(inst.memory[off:off+8], uint64(val.I64))
	case 0x38:
		binary.LittleEndian.PutUint32(inst.memory[off:off+4], math.Float32bits(val.F32))
	case 0x39:
		binary.LittleEndian.PutUint64(inst.memory[off:off+8], math.Float64bits(val.F64))
	case 0x3A:
		inst.memory[off] = byte(val.I32)
	case 0x3B:
		binary.LittleEndian.PutUint16(inst.memory[off:off+2], uint16(val.I32))
	case 0x3C:
		inst.memory[off] = byte(val.I64)
	case 0x3D:
		binary.LittleEndian.PutUint16(inst.memory[off:off+2], uint16(val.I64))
	case 0x3E:
		binary.LittleEndian.PutUint32(inst.memory[off:off+4], uint32(val.I64))
	default:
		return newError("validation_failed", fmt.Sprintf("unknown store op 0x%02x", op))
	}
	return nil
}

// Canonical NaNs per section 4.3.3
const (
	canonicalF32NaNBits uint32 = 0x7fc00000
	canonicalF64NaNBits uint64 = 0x7ff8000000000000
)

func canonicalF32(v float32) float32 {
	if math.IsNaN(float64(v)) {
		return math.Float32frombits(canonicalF32NaNBits)
	}
	return v
}

func canonicalF64(v float64) float64 {
	if math.IsNaN(v) {
		return math.Float64frombits(canonicalF64NaNBits)
	}
	return v
}

func executeNumeric(op byte, stack []Value) ([]Value, error) {
	// Every numeric instruction pops at least one operand; the binary cases check for two.
	if len(stack) == 0 {
		return nil, newError("validation_failed", "stack underflow")
	}
	switch op {
	// i32 unary
	case 0x45: // i32.eqz
		if len(stack) < 1 {
			return nil, newError("validation_failed", "stack underflow")
		}
		v := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if v.I32 == 0 {
			return append(stack, I32(1)), nil
		}
		return append(stack, I32(0)), nil
	case 0x67: // i32.clz
		if len(stack) < 1 {
			return nil, newError("validation_failed", "stack underflow")
		}
		v := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		return append(stack, I32(int32(bits.LeadingZeros32(uint32(v.I32))))), nil
	case 0x68: // i32.ctz
		if len(stack) < 1 {
			return nil, newError("validation_failed", "stack underflow")
		}
		v := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		return append(stack, I32(int32(bits.TrailingZeros32(uint32(v.I32))))), nil
	case 0x69: // i32.popcnt
		if len(stack) < 1 {
			return nil, newError("validation_failed", "stack underflow")
		}
		v := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		return append(stack, I32(int32(bits.OnesCount32(uint32(v.I32))))), nil

	// i32 binary
	case 0x6A, 0x6B, 0x6C, 0x6D, 0x6E, 0x6F, 0x70, 0x71, 0x72, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78:
		if len(stack) < 2 {
			return nil, newError("validation_failed", "stack underflow")
		}
		r := stack[len(stack)-1].I32
		l := stack[len(stack)-2].I32
		stack = stack[:len(stack)-2]
		switch op {
		case 0x6A: // i32.add
			return append(stack, I32(l+r)), nil
		case 0x6B: // i32.sub
			return append(stack, I32(l-r)), nil
		case 0x6C: // i32.mul
			return append(stack, I32(l*r)), nil
		case 0x6D: // i32.div_s
			if r == 0 {
				return nil, newTrap("integer_divide_by_zero", "integer divide by zero")
			}
			if l == math.MinInt32 && r == -1 {
				return nil, newTrap("integer_overflow", "integer overflow")
			}
			return append(stack, I32(l/r)), nil
		case 0x6E: // i32.div_u
			if r == 0 {
				return nil, newTrap("integer_divide_by_zero", "integer divide by zero")
			}
			return append(stack, I32(int32(uint32(l)/uint32(r)))), nil
		case 0x6F: // i32.rem_s
			if r == 0 {
				return nil, newTrap("integer_divide_by_zero", "integer divide by zero")
			}
			if l == math.MinInt32 && r == -1 {
				return append(stack, I32(0)), nil
			}
			return append(stack, I32(l%r)), nil
		case 0x70: // i32.rem_u
			if r == 0 {
				return nil, newTrap("integer_divide_by_zero", "integer divide by zero")
			}
			return append(stack, I32(int32(uint32(l)%uint32(r)))), nil
		case 0x71: // i32.and
			return append(stack, I32(l&r)), nil
		case 0x72: // i32.or
			return append(stack, I32(l|r)), nil
		case 0x73: // i32.xor
			return append(stack, I32(l^r)), nil
		case 0x74: // i32.shl
			return append(stack, I32(l<<uint(uint32(r)%32))), nil
		case 0x75: // i32.shr_s
			return append(stack, I32(l>>uint(uint32(r)%32))), nil
		case 0x76: // i32.shr_u
			return append(stack, I32(int32(uint32(l)>>uint(uint32(r)%32)))), nil
		case 0x77: // i32.rotl
			return append(stack, I32(int32(bits.RotateLeft32(uint32(l), int(r))))), nil
		case 0x78: // i32.rotr
			return append(stack, I32(int32(bits.RotateLeft32(uint32(l), -int(r))))), nil
		}

	// i32 comparisons
	case 0x46, 0x47, 0x48, 0x49, 0x4A, 0x4B, 0x4C, 0x4D, 0x4E, 0x4F:
		if len(stack) < 2 {
			return nil, newError("validation_failed", "stack underflow")
		}
		r := stack[len(stack)-1].I32
		l := stack[len(stack)-2].I32
		stack = stack[:len(stack)-2]
		var res bool
		switch op {
		case 0x46:
			res = (l == r) // eq
		case 0x47:
			res = (l != r) // ne
		case 0x48:
			res = (l < r) // lt_s
		case 0x49:
			res = (uint32(l) < uint32(r)) // lt_u
		case 0x4A:
			res = (l > r) // gt_s
		case 0x4B:
			res = (uint32(l) > uint32(r)) // gt_u
		case 0x4C:
			res = (l <= r) // le_s
		case 0x4D:
			res = (uint32(l) <= uint32(r)) // le_u
		case 0x4E:
			res = (l >= r) // ge_s
		case 0x4F:
			res = (uint32(l) >= uint32(r)) // ge_u
		}
		if res {
			return append(stack, I32(1)), nil
		}
		return append(stack, I32(0)), nil

	// i64 unary
	case 0x50: // i64.eqz
		if len(stack) < 1 {
			return nil, newError("validation_failed", "stack underflow")
		}
		v := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if v.I64 == 0 {
			return append(stack, I32(1)), nil
		}
		return append(stack, I32(0)), nil
	case 0x79: // i64.clz
		v := stack[len(stack)-1].I64
		stack = stack[:len(stack)-1]
		return append(stack, I64(int64(bits.LeadingZeros64(uint64(v))))), nil
	case 0x7A: // i64.ctz
		v := stack[len(stack)-1].I64
		stack = stack[:len(stack)-1]
		return append(stack, I64(int64(bits.TrailingZeros64(uint64(v))))), nil
	case 0x7B: // i64.popcnt
		v := stack[len(stack)-1].I64
		stack = stack[:len(stack)-1]
		return append(stack, I64(int64(bits.OnesCount64(uint64(v))))), nil

	// i64 binary
	case 0x7C, 0x7D, 0x7E, 0x7F, 0x80, 0x81, 0x82, 0x83, 0x84, 0x85, 0x86, 0x87, 0x88, 0x89, 0x8A:
		if len(stack) < 2 {
			return nil, newError("validation_failed", "stack underflow")
		}
		r := stack[len(stack)-1].I64
		l := stack[len(stack)-2].I64
		stack = stack[:len(stack)-2]
		switch op {
		case 0x7C: // i64.add
			return append(stack, I64(l+r)), nil
		case 0x7D: // i64.sub
			return append(stack, I64(l-r)), nil
		case 0x7E: // i64.mul
			return append(stack, I64(l*r)), nil
		case 0x7F: // i64.div_s
			if r == 0 {
				return nil, newTrap("integer_divide_by_zero", "integer divide by zero")
			}
			if l == math.MinInt64 && r == -1 {
				return nil, newTrap("integer_overflow", "integer overflow")
			}
			return append(stack, I64(l/r)), nil
		case 0x80: // i64.div_u
			if r == 0 {
				return nil, newTrap("integer_divide_by_zero", "integer divide by zero")
			}
			return append(stack, I64(int64(uint64(l)/uint64(r)))), nil
		case 0x81: // i64.rem_s
			if r == 0 {
				return nil, newTrap("integer_divide_by_zero", "integer divide by zero")
			}
			if l == math.MinInt64 && r == -1 {
				return append(stack, I64(0)), nil
			}
			return append(stack, I64(l%r)), nil
		case 0x82: // i64.rem_u
			if r == 0 {
				return nil, newTrap("integer_divide_by_zero", "integer divide by zero")
			}
			return append(stack, I64(int64(uint64(l)%uint64(r)))), nil
		case 0x83: // i64.and
			return append(stack, I64(l&r)), nil
		case 0x84: // i64.or
			return append(stack, I64(l|r)), nil
		case 0x85: // i64.xor
			return append(stack, I64(l^r)), nil
		case 0x86: // i64.shl
			return append(stack, I64(l<<uint(uint64(r)%64))), nil
		case 0x87: // i64.shr_s
			return append(stack, I64(l>>uint(uint64(r)%64))), nil
		case 0x88: // i64.shr_u
			return append(stack, I64(int64(uint64(l)>>uint(uint64(r)%64)))), nil
		case 0x89: // i64.rotl
			return append(stack, I64(int64(bits.RotateLeft64(uint64(l), int(r))))), nil
		case 0x8A: // i64.rotr
			return append(stack, I64(int64(bits.RotateLeft64(uint64(l), -int(r))))), nil
		}

	// i64 comparisons
	case 0x51, 0x52, 0x53, 0x54, 0x55, 0x56, 0x57, 0x58, 0x59, 0x5A:
		if len(stack) < 2 {
			return nil, newError("validation_failed", "stack underflow")
		}
		r := stack[len(stack)-1].I64
		l := stack[len(stack)-2].I64
		stack = stack[:len(stack)-2]
		var res bool
		switch op {
		case 0x51:
			res = (l == r)
		case 0x52:
			res = (l != r)
		case 0x53:
			res = (l < r)
		case 0x54:
			res = (uint64(l) < uint64(r))
		case 0x55:
			res = (l > r)
		case 0x56:
			res = (uint64(l) > uint64(r))
		case 0x57:
			res = (l <= r)
		case 0x58:
			res = (uint64(l) <= uint64(r))
		case 0x59:
			res = (l >= r)
		case 0x5A:
			res = (uint64(l) >= uint64(r))
		}
		if res {
			return append(stack, I32(1)), nil
		}
		return append(stack, I32(0)), nil

	// f32 unary
	case 0x8B: // abs
		v := stack[len(stack)-1].F32
		stack = stack[:len(stack)-1]
		u := math.Float32bits(v) &^ (1 << 31)
		return append(stack, F32(math.Float32frombits(u))), nil
	case 0x8C: // neg
		v := stack[len(stack)-1].F32
		stack = stack[:len(stack)-1]
		u := math.Float32bits(v) ^ (1 << 31)
		return append(stack, F32(math.Float32frombits(u))), nil
	case 0x8D: // ceil
		v := stack[len(stack)-1].F32
		stack = stack[:len(stack)-1]
		return append(stack, F32(float32(math.Ceil(float64(v))))), nil
	case 0x8E: // floor
		v := stack[len(stack)-1].F32
		stack = stack[:len(stack)-1]
		return append(stack, F32(float32(math.Floor(float64(v))))), nil
	case 0x8F: // trunc
		v := stack[len(stack)-1].F32
		stack = stack[:len(stack)-1]
		return append(stack, F32(float32(math.Trunc(float64(v))))), nil
	case 0x90: // nearest
		v := stack[len(stack)-1].F32
		stack = stack[:len(stack)-1]
		return append(stack, F32(float32(math.RoundToEven(float64(v))))), nil
	case 0x91: // sqrt
		v := stack[len(stack)-1].F32
		stack = stack[:len(stack)-1]
		return append(stack, F32(canonicalF32(float32(math.Sqrt(float64(v)))))), nil

	// f32 binary
	case 0x92, 0x93, 0x94, 0x95, 0x96, 0x97, 0x98:
		if len(stack) < 2 {
			return nil, newError("validation_failed", "stack underflow")
		}
		r := stack[len(stack)-1].F32
		l := stack[len(stack)-2].F32
		stack = stack[:len(stack)-2]
		switch op {
		case 0x92:
			return append(stack, F32(canonicalF32(l+r))), nil
		case 0x93:
			return append(stack, F32(canonicalF32(l-r))), nil
		case 0x94:
			return append(stack, F32(canonicalF32(l*r))), nil
		case 0x95:
			return append(stack, F32(canonicalF32(l/r))), nil
		case 0x96: // min
			if math.IsNaN(float64(l)) || math.IsNaN(float64(r)) {
				return append(stack, F32(canonicalF32(float32(math.NaN())))), nil
			}
			if l == 0 && r == 0 {
				if math.Signbit(float64(l)) || math.Signbit(float64(r)) {
					return append(stack, F32(float32(math.Copysign(0, -1)))), nil
				}
			}
			return append(stack, F32(float32(math.Min(float64(l), float64(r))))), nil
		case 0x97: // max
			if math.IsNaN(float64(l)) || math.IsNaN(float64(r)) {
				return append(stack, F32(canonicalF32(float32(math.NaN())))), nil
			}
			if l == 0 && r == 0 {
				if !math.Signbit(float64(l)) || !math.Signbit(float64(r)) {
					return append(stack, F32(0)), nil
				}
			}
			return append(stack, F32(float32(math.Max(float64(l), float64(r))))), nil
		case 0x98: // copysign
			return append(stack, F32(float32(math.Copysign(float64(l), float64(r))))), nil
		}

	// f32 comparisons
	case 0x5B, 0x5C, 0x5D, 0x5E, 0x5F, 0x60:
		if len(stack) < 2 {
			return nil, newError("validation_failed", "stack underflow")
		}
		r := stack[len(stack)-1].F32
		l := stack[len(stack)-2].F32
		stack = stack[:len(stack)-2]
		var res bool
		switch op {
		case 0x5B:
			res = (l == r)
		case 0x5C:
			res = (l != r)
		case 0x5D:
			res = (l < r)
		case 0x5E:
			res = (l > r)
		case 0x5F:
			res = (l <= r)
		case 0x60:
			res = (l >= r)
		}
		if res {
			return append(stack, I32(1)), nil
		}
		return append(stack, I32(0)), nil

	// f64 unary
	case 0x99: // abs
		v := stack[len(stack)-1].F64
		stack = stack[:len(stack)-1]
		u := math.Float64bits(v) &^ (uint64(1) << 63)
		return append(stack, F64(math.Float64frombits(u))), nil
	case 0x9A: // neg
		v := stack[len(stack)-1].F64
		stack = stack[:len(stack)-1]
		u := math.Float64bits(v) ^ (uint64(1) << 63)
		return append(stack, F64(math.Float64frombits(u))), nil
	case 0x9B: // ceil
		v := stack[len(stack)-1].F64
		stack = stack[:len(stack)-1]
		return append(stack, F64(math.Ceil(v))), nil
	case 0x9C: // floor
		v := stack[len(stack)-1].F64
		stack = stack[:len(stack)-1]
		return append(stack, F64(math.Floor(v))), nil
	case 0x9D: // trunc
		v := stack[len(stack)-1].F64
		stack = stack[:len(stack)-1]
		return append(stack, F64(math.Trunc(v))), nil
	case 0x9E: // nearest
		v := stack[len(stack)-1].F64
		stack = stack[:len(stack)-1]
		return append(stack, F64(math.RoundToEven(v))), nil
	case 0x9F: // sqrt
		v := stack[len(stack)-1].F64
		stack = stack[:len(stack)-1]
		return append(stack, F64(canonicalF64(math.Sqrt(v)))), nil

	// f64 binary
	case 0xA0, 0xA1, 0xA2, 0xA3, 0xA4, 0xA5, 0xA6:
		if len(stack) < 2 {
			return nil, newError("validation_failed", "stack underflow")
		}
		r := stack[len(stack)-1].F64
		l := stack[len(stack)-2].F64
		stack = stack[:len(stack)-2]
		switch op {
		case 0xA0:
			return append(stack, F64(canonicalF64(l+r))), nil
		case 0xA1:
			return append(stack, F64(canonicalF64(l-r))), nil
		case 0xA2:
			return append(stack, F64(canonicalF64(l*r))), nil
		case 0xA3:
			return append(stack, F64(canonicalF64(l/r))), nil
		case 0xA4: // min
			if math.IsNaN(l) || math.IsNaN(r) {
				return append(stack, F64(canonicalF64(math.NaN()))), nil
			}
			if l == 0 && r == 0 {
				if math.Signbit(l) || math.Signbit(r) {
					return append(stack, F64(math.Copysign(0, -1))), nil
				}
			}
			return append(stack, F64(math.Min(l, r))), nil
		case 0xA5: // max
			if math.IsNaN(l) || math.IsNaN(r) {
				return append(stack, F64(canonicalF64(math.NaN()))), nil
			}
			if l == 0 && r == 0 {
				if !math.Signbit(l) || !math.Signbit(r) {
					return append(stack, F64(0)), nil
				}
			}
			return append(stack, F64(math.Max(l, r))), nil
		case 0xA6: // copysign
			return append(stack, F64(math.Copysign(l, r))), nil
		}

	// f64 comparisons
	case 0x61, 0x62, 0x63, 0x64, 0x65, 0x66:
		if len(stack) < 2 {
			return nil, newError("validation_failed", "stack underflow")
		}
		r := stack[len(stack)-1].F64
		l := stack[len(stack)-2].F64
		stack = stack[:len(stack)-2]
		var res bool
		switch op {
		case 0x61:
			res = (l == r)
		case 0x62:
			res = (l != r)
		case 0x63:
			res = (l < r)
		case 0x64:
			res = (l > r)
		case 0x65:
			res = (l <= r)
		case 0x66:
			res = (l >= r)
		}
		if res {
			return append(stack, I32(1)), nil
		}
		return append(stack, I32(0)), nil

	// Conversions
	case 0xA7: // i32.wrap_i64
		v := stack[len(stack)-1].I64
		stack = stack[:len(stack)-1]
		return append(stack, I32(int32(v))), nil
	case 0xA8: // i32.trunc_f32_s
		v := float64(stack[len(stack)-1].F32)
		stack = stack[:len(stack)-1]
		if math.IsNaN(v) || v <= -2147483649.0 || v >= 2147483648.0 {
			return nil, newTrap("invalid_conversion", "invalid conversion to integer")
		}
		return append(stack, I32(int32(v))), nil
	case 0xA9: // i32.trunc_f32_u
		v := float64(stack[len(stack)-1].F32)
		stack = stack[:len(stack)-1]
		if math.IsNaN(v) || v <= -1.0 || v >= 4294967296.0 {
			return nil, newTrap("invalid_conversion", "invalid conversion to integer")
		}
		return append(stack, I32(int32(uint32(v)))), nil
	case 0xAA: // i32.trunc_f64_s
		v := stack[len(stack)-1].F64
		stack = stack[:len(stack)-1]
		if math.IsNaN(v) || v <= -2147483649.0 || v >= 2147483648.0 {
			return nil, newTrap("invalid_conversion", "invalid conversion to integer")
		}
		return append(stack, I32(int32(v))), nil
	case 0xAB: // i32.trunc_f64_u
		v := stack[len(stack)-1].F64
		stack = stack[:len(stack)-1]
		if math.IsNaN(v) || v <= -1.0 || v >= 4294967296.0 {
			return nil, newTrap("invalid_conversion", "invalid conversion to integer")
		}
		return append(stack, I32(int32(uint32(v)))), nil
	case 0xAC: // i64.extend_i32_s
		v := stack[len(stack)-1].I32
		stack = stack[:len(stack)-1]
		return append(stack, I64(int64(v))), nil
	case 0xAD: // i64.extend_i32_u
		v := stack[len(stack)-1].I32
		stack = stack[:len(stack)-1]
		return append(stack, I64(int64(uint32(v)))), nil
	case 0xAE: // i64.trunc_f32_s
		v := float64(stack[len(stack)-1].F32)
		stack = stack[:len(stack)-1]
		if math.IsNaN(v) || v <= -9223372036854777856.0 || v >= 9223372036854775808.0 {
			return nil, newTrap("invalid_conversion", "invalid conversion to integer")
		}
		return append(stack, I64(int64(v))), nil
	case 0xAF: // i64.trunc_f32_u
		v := float64(stack[len(stack)-1].F32)
		stack = stack[:len(stack)-1]
		if math.IsNaN(v) || v <= -1.0 || v >= 18446744073709551616.0 {
			return nil, newTrap("invalid_conversion", "invalid conversion to integer")
		}
		return append(stack, I64(int64(uint64(v)))), nil
	case 0xB0: // i64.trunc_f64_s
		v := stack[len(stack)-1].F64
		stack = stack[:len(stack)-1]
		if math.IsNaN(v) || v <= -9223372036854777856.0 || v >= 9223372036854775808.0 {
			return nil, newTrap("invalid_conversion", "invalid conversion to integer")
		}
		return append(stack, I64(int64(v))), nil
	case 0xB1: // i64.trunc_f64_u
		v := stack[len(stack)-1].F64
		stack = stack[:len(stack)-1]
		if math.IsNaN(v) || v <= -1.0 || v >= 18446744073709551616.0 {
			return nil, newTrap("invalid_conversion", "invalid conversion to integer")
		}
		return append(stack, I64(int64(uint64(v)))), nil
	case 0xB2: // f32.convert_i32_s
		v := stack[len(stack)-1].I32
		stack = stack[:len(stack)-1]
		return append(stack, F32(float32(v))), nil
	case 0xB3: // f32.convert_i32_u
		v := stack[len(stack)-1].I32
		stack = stack[:len(stack)-1]
		return append(stack, F32(float32(uint32(v)))), nil
	case 0xB4: // f32.convert_i64_s
		v := stack[len(stack)-1].I64
		stack = stack[:len(stack)-1]
		return append(stack, F32(float32(v))), nil
	case 0xB5: // f32.convert_i64_u
		v := stack[len(stack)-1].I64
		stack = stack[:len(stack)-1]
		return append(stack, F32(float32(uint64(v)))), nil
	case 0xB6: // f32.demote_f64
		v := stack[len(stack)-1].F64
		stack = stack[:len(stack)-1]
		return append(stack, F32(canonicalF32(float32(v)))), nil
	case 0xB7: // f64.convert_i32_s
		v := stack[len(stack)-1].I32
		stack = stack[:len(stack)-1]
		return append(stack, F64(float64(v))), nil
	case 0xB8: // f64.convert_i32_u
		v := stack[len(stack)-1].I32
		stack = stack[:len(stack)-1]
		return append(stack, F64(float64(uint32(v)))), nil
	case 0xB9: // f64.convert_i64_s
		v := stack[len(stack)-1].I64
		stack = stack[:len(stack)-1]
		return append(stack, F64(float64(v))), nil
	case 0xBA: // f64.convert_i64_u
		v := stack[len(stack)-1].I64
		stack = stack[:len(stack)-1]
		return append(stack, F64(float64(uint64(v)))), nil
	case 0xBB: // f64.promote_f32
		v := stack[len(stack)-1].F32
		stack = stack[:len(stack)-1]
		return append(stack, F64(canonicalF64(float64(v)))), nil
	case 0xBC: // i32.reinterpret_f32
		v := stack[len(stack)-1].F32
		stack = stack[:len(stack)-1]
		return append(stack, I32(int32(math.Float32bits(v)))), nil
	case 0xBD: // i64.reinterpret_f64
		v := stack[len(stack)-1].F64
		stack = stack[:len(stack)-1]
		return append(stack, I64(int64(math.Float64bits(v)))), nil
	case 0xBE: // f32.reinterpret_i32
		v := stack[len(stack)-1].I32
		stack = stack[:len(stack)-1]
		return append(stack, F32(math.Float32frombits(uint32(v)))), nil
	case 0xBF: // f64.reinterpret_i64
		v := stack[len(stack)-1].I64
		stack = stack[:len(stack)-1]
		return append(stack, F64(math.Float64frombits(uint64(v)))), nil
	}

	return nil, newError("validation_failed", fmt.Sprintf("unsupported instruction op 0x%02x", op))
}

// Decoder implementation
type reader struct {
	data []byte
	pos  int
}

func (r *reader) remaining() int {
	return len(r.data) - r.pos
}

func (r *reader) readByte() (byte, error) {
	if r.pos >= len(r.data) {
		return 0, io.ErrUnexpectedEOF
	}
	b := r.data[r.pos]
	r.pos++
	return b, nil
}

func (r *reader) readBytes(n int) ([]byte, error) {
	if n < 0 || r.pos+n > len(r.data) {
		return nil, io.ErrUnexpectedEOF
	}
	res := r.data[r.pos : r.pos+n]
	r.pos += n
	return res, nil
}

func (r *reader) readU32() (uint32, error) {
	var res uint32
	var shift uint
	for shift < 35 {
		b, err := r.readByte()
		if err != nil {
			return 0, err
		}
		res |= uint32(b&0x7F) << shift
		if (b & 0x80) == 0 {
			return res, nil
		}
		shift += 7
	}
	return 0, newError("malformed_section", "integer representation too long")
}

func (r *reader) readI32() (int32, error) {
	var res int32
	var shift uint
	var b byte
	for shift < 35 {
		var err error
		b, err = r.readByte()
		if err != nil {
			return 0, err
		}
		res |= int32(b&0x7F) << shift
		shift += 7
		if (b & 0x80) == 0 {
			if shift < 32 && (b&0x40) != 0 {
				res |= ^0 << shift
			}
			return res, nil
		}
	}
	return 0, newError("malformed_section", "integer representation too long")
}

func (r *reader) readI64() (int64, error) {
	var res int64
	var shift uint
	var b byte
	for shift < 70 {
		var err error
		b, err = r.readByte()
		if err != nil {
			return 0, err
		}
		res |= int64(b&0x7F) << shift
		shift += 7
		if (b & 0x80) == 0 {
			if shift < 64 && (b&0x40) != 0 {
				res |= ^0 << shift
			}
			return res, nil
		}
	}
	return 0, newError("malformed_section", "integer representation too long")
}

func (r *reader) readString() (string, error) {
	n, err := r.readU32()
	if err != nil {
		return "", err
	}
	bytes, err := r.readBytes(int(n))
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func (r *reader) readLimits() (Limits, error) {
	flags, err := r.readByte()
	if err != nil {
		return Limits{}, err
	}
	min, err := r.readU32()
	if err != nil {
		return Limits{}, err
	}
	var max *uint32
	if flags == 1 {
		m, err := r.readU32()
		if err != nil {
			return Limits{}, err
		}
		max = &m
	} else if flags != 0 {
		return Limits{}, newError("malformed_section", "invalid limit flag")
	}
	return Limits{Min: min, Max: max}, nil
}

func decodeModule(wasm []byte, cfg Config) (*Module, error) {
	r := &reader{data: wasm}
	magic, err := r.readBytes(4)
	if err != nil || string(magic) != "\x00asm" {
		return nil, newError("invalid_magic", "magic header not detected")
	}
	versionBytes, err := r.readBytes(4)
	if err != nil {
		return nil, newError("malformed_section", "truncated version")
	}
	ver := binary.LittleEndian.Uint32(versionBytes)
	if ver != 1 {
		return nil, newError("invalid_version", fmt.Sprintf("unsupported version %d", ver))
	}

	mod := &Module{Config: cfg}
	lastSecId := byte(0)
	for r.remaining() > 0 {
		secId, err := r.readByte()
		if err != nil {
			return nil, newError("malformed_section", "error reading section ID")
		}
		secLen, err := r.readU32()
		if err != nil {
			return nil, newError("malformed_section", "error reading section size")
		}
		secBytes, err := r.readBytes(int(secLen))
		if err != nil {
			return nil, newError("malformed_section", "truncated section content")
		}
		if secId != 0 {
			if secId <= lastSecId {
				return nil, newError("malformed_section", "sections out of order")
			}
			lastSecId = secId
		}
		sr := &reader{data: secBytes}
		switch secId {
		case 0: // custom section
			// custom sections are skipped in MVP execution
		case 1: // type section
			count, err := sr.readU32()
			if err != nil {
				return nil, newError("malformed_section", "malformed type section count")
			}
			for i := uint32(0); i < count; i++ {
				ft, err := decodeFuncType(sr)
				if err != nil {
					return nil, err
				}
				mod.Types = append(mod.Types, ft)
			}
		case 2: // import section
			count, err := sr.readU32()
			if err != nil {
				return nil, newError("malformed_section", "malformed import section count")
			}
			for i := uint32(0); i < count; i++ {
				imp, err := decodeImport(sr)
				if err != nil {
					return nil, err
				}
				mod.Imports = append(mod.Imports, imp)
				if imp.Desc.Kind == ImportKindFunc {
					mod.ImportFuncs = append(mod.ImportFuncs, imp.Desc.TypeIdx)
				}
			}
		case 3: // func section
			count, err := sr.readU32()
			if err != nil {
				return nil, newError("malformed_section", "malformed func section count")
			}
			for i := uint32(0); i < count; i++ {
				typeIdx, err := sr.readU32()
				if err != nil {
					return nil, newError("malformed_section", "malformed type index in func section")
				}
				mod.Funcs = append(mod.Funcs, typeIdx)
			}
		case 4: // table section
			count, err := sr.readU32()
			if err != nil {
				return nil, newError("malformed_section", "malformed table count")
			}
			for i := uint32(0); i < count; i++ {
				elemType, err := sr.readByte()
				if err != nil {
					return nil, newError("malformed_section", "error reading table elem type")
				}
				limits, err := sr.readLimits()
				if err != nil {
					return nil, err
				}
				mod.Tables = append(mod.Tables, TableType{ElemType: elemType, Limits: limits})
			}
		case 5: // memory section
			count, err := sr.readU32()
			if err != nil {
				return nil, newError("malformed_section", "malformed mem section count")
			}
			for i := uint32(0); i < count; i++ {
				limits, err := sr.readLimits()
				if err != nil {
					return nil, err
				}
				mod.Memories = append(mod.Memories, MemType{Limits: limits})
			}
		case 6: // global section
			count, err := sr.readU32()
			if err != nil {
				return nil, newError("malformed_section", "malformed global section count")
			}
			for i := uint32(0); i < count; i++ {
				vt, err := sr.readByte()
				if err != nil {
					return nil, newError("malformed_section", "malformed global type")
				}
				mut, err := sr.readByte()
				if err != nil {
					return nil, newError("malformed_section", "malformed global mutability")
				}
				expr, err := decodeExpr(sr)
				if err != nil {
					return nil, err
				}
				mod.Globals = append(mod.Globals, GlobalDecl{
					Type: GlobalType{ValType: ValType(vt), Mutable: mut == 1},
					Init: expr,
				})
			}
		case 7: // export section
			count, err := sr.readU32()
			if err != nil {
				return nil, newError("malformed_section", "malformed export section count")
			}
			for i := uint32(0); i < count; i++ {
				name, err := sr.readString()
				if err != nil {
					return nil, err
				}
				kind, err := sr.readByte()
				if err != nil {
					return nil, newError("malformed_section", "malformed export kind")
				}
				idx, err := sr.readU32()
				if err != nil {
					return nil, newError("malformed_section", "malformed export index")
				}
				mod.Exports = append(mod.Exports, Export{Name: name, Kind: kind, Idx: idx})
			}
		case 8: // start section
			idx, err := sr.readU32()
			if err != nil {
				return nil, newError("malformed_section", "malformed start func index")
			}
			mod.Start = &idx
		case 9: // elem section
			count, err := sr.readU32()
			if err != nil {
				return nil, newError("malformed_section", "malformed elem section count")
			}
			for i := uint32(0); i < count; i++ {
				tblIdx, err := sr.readU32()
				if err != nil {
					return nil, newError("malformed_section", "malformed table idx in elem")
				}
				off, err := decodeExpr(sr)
				if err != nil {
					return nil, err
				}
				numElems, err := sr.readU32()
				if err != nil {
					return nil, newError("malformed_section", "malformed elem count")
				}
				if int64(numElems) > int64(sr.remaining()) {
					return nil, newError("malformed_section", "elem count exceeds section size")
				}
				elems := make([]uint32, numElems)
				for e := uint32(0); e < numElems; e++ {
					elems[e], err = sr.readU32()
					if err != nil {
						return nil, newError("malformed_section", "malformed elem item")
					}
				}
				mod.Elements = append(mod.Elements, ElementSegment{
					TableIdx: tblIdx,
					Offset:   off,
					Init:     elems,
				})
			}
		case 10: // code section
			count, err := sr.readU32()
			if err != nil {
				return nil, newError("malformed_section", "malformed code section count")
			}
			for i := uint32(0); i < count; i++ {
				bodyLen, err := sr.readU32()
				if err != nil {
					return nil, newError("malformed_section", "malformed body size")
				}
				bodyBytes, err := sr.readBytes(int(bodyLen))
				if err != nil {
					return nil, newError("malformed_section", "truncated body")
				}
				body, err := decodeFunctionBody(bodyBytes)
				if err != nil {
					return nil, err
				}
				mod.Bodies = append(mod.Bodies, body)
			}
		case 11: // data section
			count, err := sr.readU32()
			if err != nil {
				return nil, newError("malformed_section", "malformed data section count")
			}
			for i := uint32(0); i < count; i++ {
				memIdx, err := sr.readU32()
				if err != nil {
					return nil, newError("malformed_section", "malformed mem idx in data")
				}
				off, err := decodeExpr(sr)
				if err != nil {
					return nil, err
				}
				dataLen, err := sr.readU32()
				if err != nil {
					return nil, newError("malformed_section", "malformed data len")
				}
				dBytes, err := sr.readBytes(int(dataLen))
				if err != nil {
					return nil, newError("malformed_section", "truncated data segment")
				}
				dCopy := make([]byte, len(dBytes))
				copy(dCopy, dBytes)
				mod.Data = append(mod.Data, DataSegment{
					MemIdx: memIdx,
					Offset: off,
					Data:   dCopy,
				})
			}
		default:
			return nil, newError("malformed_section", fmt.Sprintf("unknown section id %d", secId))
		}
	}

	return mod, nil
}

func decodeFuncType(r *reader) (FuncType, error) {
	form, err := r.readByte()
	if err != nil || form != 0x60 {
		return FuncType{}, newError("malformed_section", "func type must start with 0x60")
	}
	paramCount, err := r.readU32()
	if err != nil {
		return FuncType{}, newError("malformed_section", "malformed param count")
	}
	if int64(paramCount) > int64(r.remaining()) {
		return FuncType{}, newError("malformed_section", "param count exceeds section size")
	}
	params := make([]ValType, paramCount)
	for i := uint32(0); i < paramCount; i++ {
		vt, err := r.readByte()
		if err != nil {
			return FuncType{}, newError("malformed_section", "malformed param type")
		}
		params[i] = ValType(vt)
	}
	resCount, err := r.readU32()
	if err != nil {
		return FuncType{}, newError("malformed_section", "malformed return count")
	}
	if int64(resCount) > int64(r.remaining()) {
		return FuncType{}, newError("malformed_section", "result count exceeds section size")
	}
	results := make([]ValType, resCount)
	for i := uint32(0); i < resCount; i++ {
		vt, err := r.readByte()
		if err != nil {
			return FuncType{}, newError("malformed_section", "malformed return type")
		}
		results[i] = ValType(vt)
	}
	return FuncType{Params: params, Results: results}, nil
}

func decodeImport(r *reader) (Import, error) {
	modName, err := r.readString()
	if err != nil {
		return Import{}, err
	}
	field, err := r.readString()
	if err != nil {
		return Import{}, err
	}
	kind, err := r.readByte()
	if err != nil {
		return Import{}, newError("malformed_section", "malformed import kind")
	}
	var desc ImportDesc
	desc.Kind = ImportKind(kind)
	switch desc.Kind {
	case ImportKindFunc:
		typeIdx, err := r.readU32()
		if err != nil {
			return Import{}, newError("malformed_section", "malformed func import type idx")
		}
		desc.TypeIdx = typeIdx
	case ImportKindTable:
		et, err := r.readByte()
		if err != nil {
			return Import{}, newError("malformed_section", "malformed table elem type in import")
		}
		lim, err := r.readLimits()
		if err != nil {
			return Import{}, err
		}
		desc.Table = TableType{ElemType: et, Limits: lim}
	case ImportKindMem:
		lim, err := r.readLimits()
		if err != nil {
			return Import{}, err
		}
		desc.Mem = MemType{Limits: lim}
	case ImportKindGlobal:
		vt, err := r.readByte()
		if err != nil {
			return Import{}, newError("malformed_section", "malformed global type in import")
		}
		mut, err := r.readByte()
		if err != nil {
			return Import{}, newError("malformed_section", "malformed global mutability in import")
		}
		desc.Global = GlobalType{ValType: ValType(vt), Mutable: mut == 1}
	default:
		return Import{}, newError("malformed_section", fmt.Sprintf("unknown import kind %d", kind))
	}
	return Import{Module: modName, Name: field, Desc: desc}, nil
}

func decodeExpr(r *reader) ([]Instr, error) {
	var instrs []Instr
	for {
		in, err := decodeInstruction(r)
		if err != nil {
			return nil, err
		}
		instrs = append(instrs, in)
		if in.Op == 0x0B {
			break
		}
	}
	return instrs, nil
}

// maxLocals bounds the locals one function may declare, so a tiny body cannot force a huge
// allocation; it matches the default MaxStackDepth.
const maxLocals = 65536

func decodeFunctionBody(bytes []byte) (FunctionBody, error) {
	r := &reader{data: bytes}
	localCount, err := r.readU32()
	if err != nil {
		return FunctionBody{}, newError("malformed_section", "malformed local group count")
	}
	var locals []ValType
	var totalLocals uint64
	for i := uint32(0); i < localCount; i++ {
		n, err := r.readU32()
		if err != nil {
			return FunctionBody{}, newError("malformed_section", "malformed local count in group")
		}
		totalLocals += uint64(n)
		if totalLocals > maxLocals {
			return FunctionBody{}, newError("limit_exceeded", fmt.Sprintf("function declares more than %d locals", maxLocals))
		}
		vt, err := r.readByte()
		if err != nil {
			return FunctionBody{}, newError("malformed_section", "malformed local type")
		}
		for j := uint32(0); j < n; j++ {
			locals = append(locals, ValType(vt))
		}
	}

	var instrs []Instr
	for r.remaining() > 0 {
		in, err := decodeInstruction(r)
		if err != nil {
			return FunctionBody{}, err
		}
		instrs = append(instrs, in)
	}

	// Link block/loop/if targets
	linkControlFlow(instrs)

	return FunctionBody{Locals: locals, Code: instrs}, nil
}

func decodeInstruction(r *reader) (Instr, error) {
	op, err := r.readByte()
	if err != nil {
		return Instr{}, err
	}
	in := Instr{Op: op}
	switch op {
	case 0x02, 0x03, 0x04: // block, loop, if
		bt, err := r.readI32()
		if err != nil {
			return Instr{}, newError("malformed_section", "malformed block type")
		}
		// 0x40 (empty) and the value types 0x7F..0x7C decode as negative signed LEB values.
		if bt < -4 && bt != blockTypeEmpty {
			return Instr{}, newError("malformed_section", "invalid block type")
		}
		in.BlockType = int64(bt)
	case 0x0C, 0x0D: // br, br_if
		depth, err := r.readU32()
		if err != nil {
			return Instr{}, newError("malformed_section", "malformed branch label")
		}
		in.U32 = depth
	case 0x0E: // br_table
		count, err := r.readU32()
		if err != nil {
			return Instr{}, newError("malformed_section", "malformed br_table count")
		}
		if int64(count) > int64(r.remaining()) {
			return Instr{}, newError("malformed_section", "br_table count exceeds body size")
		}
		in.Targets = make([]uint32, count)
		for i := uint32(0); i < count; i++ {
			in.Targets[i], err = r.readU32()
			if err != nil {
				return Instr{}, newError("malformed_section", "malformed br_table target")
			}
		}
		in.Default, err = r.readU32()
		if err != nil {
			return Instr{}, newError("malformed_section", "malformed br_table default")
		}
	case 0x10: // call
		fnIdx, err := r.readU32()
		if err != nil {
			return Instr{}, newError("malformed_section", "malformed call func index")
		}
		in.U32 = fnIdx
	case 0x11: // call_indirect
		typeIdx, err := r.readU32()
		if err != nil {
			return Instr{}, newError("malformed_section", "malformed call_indirect type index")
		}
		in.U32 = typeIdx
		tbl, err := r.readByte()
		if err != nil || tbl != 0 {
			return Instr{}, newError("malformed_section", "call_indirect reserved byte must be 0")
		}
	case 0x20, 0x21, 0x22: // local.get, local.set, local.tee
		lIdx, err := r.readU32()
		if err != nil {
			return Instr{}, newError("malformed_section", "malformed local index")
		}
		in.U32 = lIdx
	case 0x23, 0x24: // global.get, global.set
		gIdx, err := r.readU32()
		if err != nil {
			return Instr{}, newError("malformed_section", "malformed global index")
		}
		in.U32 = gIdx
	// Memory operations
	case 0x28, 0x29, 0x2A, 0x2B, 0x2C, 0x2D, 0x2E, 0x2F, 0x30, 0x31, 0x32, 0x33, 0x34, 0x35,
		0x36, 0x37, 0x38, 0x39, 0x3A, 0x3B, 0x3C, 0x3D, 0x3E:
		align, err := r.readU32()
		if err != nil {
			return Instr{}, newError("malformed_section", "malformed mem align")
		}
		offset, err := r.readU32()
		if err != nil {
			return Instr{}, newError("malformed_section", "malformed mem offset")
		}
		in.Align = align
		in.Offset = offset
	case 0x3F, 0x40: // memory.size, memory.grow
		rsv, err := r.readByte()
		if err != nil || rsv != 0 {
			return Instr{}, newError("malformed_section", "memory instruction reserved byte must be 0")
		}
	case 0x41: // i32.const
		v, err := r.readI32()
		if err != nil {
			return Instr{}, newError("malformed_section", "malformed i32.const")
		}
		in.I32 = v
	case 0x42: // i64.const
		v, err := r.readI64()
		if err != nil {
			return Instr{}, newError("malformed_section", "malformed i64.const")
		}
		in.I64 = v
	case 0x43: // f32.const
		b, err := r.readBytes(4)
		if err != nil {
			return Instr{}, newError("malformed_section", "malformed f32.const")
		}
		u := binary.LittleEndian.Uint32(b)
		in.F32 = math.Float32frombits(u)
	case 0x44: // f64.const
		b, err := r.readBytes(8)
		if err != nil {
			return Instr{}, newError("malformed_section", "malformed f64.const")
		}
		u := binary.LittleEndian.Uint64(b)
		in.F64 = math.Float64frombits(u)
	}
	return in, nil
}

type ctrlEntry struct {
	op     byte
	pc     int
	elsePc int
}

func linkControlFlow(instrs []Instr) {
	var stack []ctrlEntry
	for pc := 0; pc < len(instrs); pc++ {
		switch instrs[pc].Op {
		case 0x02, 0x03: // block, loop
			stack = append(stack, ctrlEntry{op: instrs[pc].Op, pc: pc, elsePc: -1})
		case 0x04: // if
			stack = append(stack, ctrlEntry{op: 0x04, pc: pc, elsePc: -1})
		case 0x05: // else
			if len(stack) > 0 {
				top := &stack[len(stack)-1]
				if top.op == 0x04 {
					top.elsePc = pc
					instrs[top.pc].Offset = uint32(pc + 1)
				}
			}
		case 0x0B: // end
			if len(stack) > 0 {
				top := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				if top.op == 0x02 || top.op == 0x04 {
					instrs[top.pc].U32 = uint32(pc)
					if top.op == 0x04 && top.elsePc == -1 {
						instrs[top.pc].Offset = uint32(pc)
					}
				}
			}
		}
	}
}

// Module validator
func validateModule(m *Module) error {
	if len(m.Memories) > 1 {
		return newError("validation_failed", "multiple memories not allowed in MVP")
	}
	if len(m.Tables) > 1 {
		return newError("validation_failed", "multiple tables not allowed in MVP")
	}
	for _, mem := range m.Memories {
		if mem.Limits.Max != nil && *mem.Limits.Max < mem.Limits.Min {
			return newError("validation_failed", "memory min > max")
		}
	}
	for _, tbl := range m.Tables {
		if tbl.Limits.Max != nil && *tbl.Limits.Max < tbl.Limits.Min {
			return newError("validation_failed", "table min > max")
		}
	}
	if len(m.Funcs) != len(m.Bodies) {
		return newError("validation_failed", "func and code section counts mismatch")
	}
	return nil
}
