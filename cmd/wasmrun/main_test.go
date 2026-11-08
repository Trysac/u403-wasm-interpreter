package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunCLI(t *testing.T) {
	// Build a minimal valid Wasm module exporting 'add'(i32, i32) -> i32
	wasm := []byte{
		0x00, 0x61, 0x73, 0x6D, 0x01, 0x00, 0x00, 0x00,
		0x01, 0x07, 0x01, 0x60, 0x02, 0x7F, 0x7F, 0x01, 0x7F,
		0x03, 0x02, 0x01, 0x00,
		0x07, 0x07, 0x01, 0x03, 'a', 'd', 'd', 0x00, 0x00,
		0x0A, 0x09, 0x01, 0x07, 0x00, 0x20, 0x00, 0x20, 0x01, 0x6A, 0x0B,
	}

	tmpDir := t.TempDir()
	wasmPath := filepath.Join(tmpDir, "test.wasm")
	if err := os.WriteFile(wasmPath, wasm, 0o600); err != nil {
		t.Fatalf("failed to write wasm file: %v", err)
	}

	t.Run("success", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := Run([]string{"--fuel", "1000", "--max-pages", "10", wasmPath, "add", "10", "20"}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("expected code 0, got %d, stderr: %s", code, stderr.String())
		}
		if !strings.Contains(stdout.String(), "30") {
			t.Fatalf("expected 30 in output, got %s", stdout.String())
		}
	})

	t.Run("missing args", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := Run([]string{}, &stdout, &stderr)
		if code != 2 {
			t.Fatalf("expected code 2, got %d", code)
		}
	})

	t.Run("invalid flags", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := Run([]string{"--unknown-flag"}, &stdout, &stderr)
		if code != 2 {
			t.Fatalf("expected code 2, got %d", code)
		}
	})

	t.Run("missing wasm file", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := Run([]string{filepath.Join(tmpDir, "nonexistent.wasm"), "add"}, &stdout, &stderr)
		if code != 2 {
			t.Fatalf("expected code 2, got %d", code)
		}
	})

	t.Run("invalid arg", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := Run([]string{wasmPath, "add", "notanumber"}, &stdout, &stderr)
		if code != 2 {
			t.Fatalf("expected code 2, got %d", code)
		}
	})

	t.Run("invalid module compile", func(t *testing.T) {
		badWasmPath := filepath.Join(tmpDir, "bad.wasm")
		_ = os.WriteFile(badWasmPath, []byte{0x00, 0x00}, 0o600)
		var stdout, stderr bytes.Buffer
		code := Run([]string{badWasmPath, "add"}, &stdout, &stderr)
		if code != 1 {
			t.Fatalf("expected code 1, got %d", code)
		}
	})

	t.Run("runtime error (export not found)", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := Run([]string{wasmPath, "nonexistent"}, &stdout, &stderr)
		if code != 1 {
			t.Fatalf("expected code 1, got %d", code)
		}
	})
}
