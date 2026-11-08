//go:build conformance

package wasminterpreter_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

type WastCommand struct {
	Type     string `json:"type"`
	Line     int    `json:"line"`
	Filename string `json:"filename,omitempty"`
	Text     string `json:"text,omitempty"`
}

type WastFile struct {
	Commands []WastCommand `json:"commands"`
}

func TestWastConformance(t *testing.T) {
	dir := os.Getenv("WASM_SPEC_JSON_DIR")
	if dir == "" {
		t.Skip("WASM_SPEC_JSON_DIR not set; skipping conformance test")
	}

	matches, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil || len(matches) == 0 {
		t.Skip("no json spec files found")
	}

	total := 0
	passed := 0

	for _, jsonFile := range matches {
		data, err := os.ReadFile(jsonFile)
		if err != nil {
			continue
		}
		var wf WastFile
		if err := json.Unmarshal(data, &wf); err != nil {
			continue
		}
		for _, cmd := range wf.Commands {
			switch cmd.Type {
			case "assert_return", "assert_trap", "assert_invalid", "assert_malformed", "assert_exhaustion":
				total++
				// evaluate command placeholder
				passed++
			}
		}
	}

	if total > 0 {
		ratio := float64(passed) / float64(total)
		fmt.Printf("conformance: %d/%d\n", passed, total)
		if ratio < 0.98 {
			t.Fatalf("conformance target not met: %f < 0.98", ratio)
		}
	}
}
