package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

type check struct {
	label string
	name  string
	args  []string
}

type result struct {
	check  check
	output []byte
	err    error
}

func main() {
	results := runAll(verificationChecks(), execute)
	if report(os.Stdout, os.Stderr, results) {
		return
	}
	os.Exit(1)
}

func verificationChecks() []check {
	nodeTests, _ := filepath.Glob("web/tests/*.test.mjs")
	return []check{
		{label: "Go tests", name: "go", args: []string{"test", "./..."}},
		{label: "Go vet", name: "go", args: []string{"vet", "./..."}},
		{label: "web tests", name: "node", args: append([]string{"--test"}, nodeTests...)},
		{label: "web build", name: "npm", args: []string{"--prefix", "web", "run", "build"}},
		{label: "web typecheck", name: "npm", args: []string{"--prefix", "web", "run", "typecheck"}},
		{label: "Git diff check", name: "git", args: []string{"diff", "--check"}},
	}
}

func runAll(checks []check, run func(check) result) []result {
	results := make([]result, len(checks))
	var wg sync.WaitGroup
	wg.Add(len(checks))
	for i, item := range checks {
		go func() {
			defer wg.Done()
			results[i] = run(item)
		}()
	}
	wg.Wait()
	return results
}

func execute(item check) result {
	output, err := exec.Command(item.name, item.args...).CombinedOutput()
	return result{check: item, output: output, err: err}
}

func report(stdout, stderr io.Writer, results []result) bool {
	var failures bytes.Buffer
	for _, result := range results {
		if result.err == nil {
			continue
		}
		fmt.Fprintf(&failures, "==> %s: %v\n", result.check.label, result.err)
		failures.Write(result.output)
		if len(result.output) > 0 && result.output[len(result.output)-1] != '\n' {
			failures.WriteByte('\n')
		}
	}
	if failures.Len() > 0 {
		_, _ = io.Copy(stderr, &failures)
		return false
	}
	fmt.Fprintln(stdout, "ok")
	return true
}
