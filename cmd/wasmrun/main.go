package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/Trysac/u403-wasm-interpreter"
)

func Run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("wasmrun", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fuel := fs.Uint64("fuel", 0, "Maximum execution fuel (0 for unbounded)")
	maxPages := fs.Uint("max-pages", 256, "Maximum memory pages")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	rest := fs.Args()
	if len(rest) < 2 {
		fmt.Fprintf(stderr, "Usage: wasmrun [--fuel <n>] [--max-pages <n>] <module.wasm> <func_name> [args...]\n")
		return 2
	}

	wasmPath := rest[0]
	funcName := rest[1]
	fnArgs := rest[2:]

	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		fmt.Fprintf(stderr, "Error reading file: %v\n", err)
		return 2
	}

	cfg := wasminterpreter.DefaultConfig()
	cfg.Fuel = *fuel
	cfg.MaxMemoryPages = uint32(*maxPages)

	mod, err := wasminterpreter.Compile(wasmBytes, cfg)
	if err != nil {
		fmt.Fprintf(stderr, "Compile error: %v\n", err)
		return 1
	}

	inst, err := wasminterpreter.Instantiate(mod, nil)
	if err != nil {
		fmt.Fprintf(stderr, "Instantiate error: %v\n", err)
		return 1
	}

	parsedArgs := make([]wasminterpreter.Value, len(fnArgs))
	for i, s := range fnArgs {
		v, err := strconv.ParseInt(s, 10, 32)
		if err != nil {
			fmt.Fprintf(stderr, "Invalid argument '%s': only i32 supported in CLI: %v\n", s, err)
			return 2
		}
		parsedArgs[i] = wasminterpreter.I32(int32(v))
	}

	results, err := inst.Call(funcName, parsedArgs...)
	if err != nil {
		fmt.Fprintf(stderr, "Runtime error: %v\n", err)
		return 1
	}

	for _, r := range results {
		fmt.Fprintf(stdout, "%s\n", r)
	}

	return 0
}

func main() {
	os.Exit(Run(os.Args[1:], os.Stdout, os.Stderr))
}
