package integration_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var (
	cliBinary string
	coreRoot  string
)

func TestMain(m *testing.M) {
	var err error
	coreRoot, err = filepath.Abs("..")
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve core root: %v\n", err)
		os.Exit(1)
	}

	tempDir, err := os.MkdirTemp("", "uamask-cli-test-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create CLI test directory: %v\n", err)
		os.Exit(1)
	}
	cliBinary = filepath.Join(tempDir, "UAmask")
	build := exec.Command("go", "build", "-o", cliBinary, "./cmd/UAmask")
	build.Dir = coreRoot
	if output, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build UAmask CLI: %v\n%s", err, output)
		_ = os.RemoveAll(tempDir)
		os.Exit(1)
	}

	code := m.Run()
	if err := os.RemoveAll(tempDir); err != nil && code == 0 {
		fmt.Fprintf(os.Stderr, "remove CLI test directory: %v\n", err)
		code = 1
	}
	os.Exit(code)
}

func TestCLIContracts(t *testing.T) {
	t.Run("PROC-VERSION-001/version output", func(t *testing.T) {
		result := runCLI(t, "-v")
		if result.exitCode != 0 {
			t.Fatalf("exit code = %d, stderr=%q", result.exitCode, result.stderr)
		}
		if !strings.HasPrefix(result.stdout, "UA-Mask version: ") || !strings.HasSuffix(result.stdout, "\n") {
			t.Fatalf("unexpected version output %q", result.stdout)
		}
		if result.stderr != "" {
			t.Fatalf("unexpected stderr %q", result.stderr)
		}
	})

	t.Run("PROC-CHECK-001/valid config", func(t *testing.T) {
		path := configFixture(t, "valid", "schema-v1-full.json")
		result := runCLI(t, "-check-config", "-config", path)
		if result.exitCode != 0 || result.stdout != "configuration valid\n" || result.stderr != "" {
			t.Fatalf("unexpected result: %+v", result)
		}
	})

	t.Run("PROC-CHECK-002/invalid config includes path", func(t *testing.T) {
		path := configFixture(t, "invalid", "cfg-null-001-null-scalar.json")
		result := runCLI(t, "-check-config", "-config", path)
		if result.exitCode == 0 {
			t.Fatalf("expected failure, got %+v", result)
		}
		if !strings.Contains(result.stderr, path) || !strings.Contains(result.stderr, "null is not allowed at $.listen.port") {
			t.Fatalf("stderr %q does not identify the file and field", result.stderr)
		}
	})

	t.Run("PROC-DUMP-001/effective config reloads", func(t *testing.T) {
		path := configFixture(t, "valid", "schema-v1-full.json")
		dump := runCLI(t, "-dump-effective-config", "-config", path)
		if dump.exitCode != 0 || dump.stderr != "" {
			t.Fatalf("dump failed: %+v", dump)
		}

		var document map[string]any
		if err := json.Unmarshal([]byte(dump.stdout), &document); err != nil {
			t.Fatalf("dump is not JSON: %v\n%s", err, dump.stdout)
		}
		if document["schema_version"] != float64(1) {
			t.Fatalf("schema_version = %v, want 1", document["schema_version"])
		}

		dumpedPath := filepath.Join(t.TempDir(), "effective.json")
		if err := os.WriteFile(dumpedPath, []byte(dump.stdout), 0o600); err != nil {
			t.Fatalf("write dumped config: %v", err)
		}
		check := runCLI(t, "-check-config", "-config", dumpedPath)
		if check.exitCode != 0 || check.stdout != "configuration valid\n" {
			t.Fatalf("dumped config did not reload: %+v", check)
		}
	})

	t.Run("PROC-ARGS-001/unknown flag", func(t *testing.T) {
		result := runCLI(t, "-not-a-real-flag")
		if result.exitCode == 0 || !strings.Contains(result.stderr, "flag provided but not defined") {
			t.Fatalf("unexpected result: %+v", result)
		}
	})

	t.Run("PROC-ARGS-002/positional argument", func(t *testing.T) {
		result := runCLI(t, "unexpected")
		if result.exitCode == 0 || !strings.Contains(result.stderr, "unexpected positional arguments") {
			t.Fatalf("unexpected result: %+v", result)
		}
	})
}

type cliResult struct {
	exitCode int
	stdout   string
	stderr   string
}

func runCLI(t *testing.T, args ...string) cliResult {
	t.Helper()
	command := exec.Command(cliBinary, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	exitCode := 0
	if err != nil {
		var exitError *exec.ExitError
		if !errors.As(err, &exitError) {
			t.Fatalf("run CLI: %v", err)
		}
		exitCode = exitError.ExitCode()
	}
	return cliResult{exitCode: exitCode, stdout: stdout.String(), stderr: stderr.String()}
}

func configFixture(t *testing.T, category, name string) string {
	t.Helper()
	path := filepath.Join(coreRoot, "internal", "config", "testdata", category, name)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stat fixture %q: %v", path, err)
	}
	return path
}
