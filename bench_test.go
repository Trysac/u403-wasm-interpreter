package wasminterpreter_test

import (
	"testing"

	"github.com/Trysac/u403-wasm-interpreter"
)

func BenchmarkAdd(b *testing.B) {
	wasm := append(wasmHeader(), []byte{
		0x01, 0x05, 0x01, 0x60, 0x00, 0x01, 0x7F,
		0x03, 0x02, 0x01, 0x00,
		0x07, 0x07, 0x01, 0x03, 'a', 'd', 'd', 0x00, 0x00,
		0x0A, 0x09, 0x01, 0x07, 0x00, 0x41, 0x14, 0x41, 0x16, 0x6A, 0x0B,
	}...)
	mod, err := wasminterpreter.Compile(wasm, wasminterpreter.DefaultConfig())
	if err != nil {
		b.Fatalf("compile failed: %v", err)
	}
	inst, err := wasminterpreter.Instantiate(mod, nil)
	if err != nil {
		b.Fatalf("instantiate failed: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := inst.Call("add")
		if err != nil {
			b.Fatalf("call error: %v", err)
		}
	}
}
