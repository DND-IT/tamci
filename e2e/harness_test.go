// Package e2e runs the tamci binary as a subprocess, the way the Docker
// actions do, against local git remotes and fake HTTP services.
package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var bin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "tamci-e2e-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	bin = filepath.Join(dir, "tamci")
	build := exec.Command("go", "build", "-o", bin, "./cmd/tamci")
	build.Dir = ".."
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build tamci: %v\n%s", err, out)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

type result struct {
	outputs map[string]string
	state   map[string]string
	log     string
	err     error
}

// tamci runs the binary in dir with env on top of a sanitized copy of the
// test's environment: runner variables of the host job are dropped so only
// what the test sets reaches the binary.
func tamci(t *testing.T, dir string, env map[string]string, args ...string) result {
	t.Helper()
	tmp := t.TempDir()
	outputFile := filepath.Join(tmp, "output")
	stateFile := filepath.Join(tmp, "state")

	var environ []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		switch {
		case strings.HasPrefix(name, "GITHUB_"), strings.HasPrefix(name, "INPUT_"),
			strings.HasPrefix(name, "ACTIONS_"), strings.HasPrefix(name, "RUNNER_"),
			strings.HasPrefix(name, "STATE_"):
			continue
		}
		environ = append(environ, kv)
	}
	environ = append(environ,
		"GITHUB_ACTIONS=true",
		"GITHUB_OUTPUT="+outputFile,
		"GITHUB_STATE="+stateFile,
		"GITHUB_STEP_SUMMARY="+filepath.Join(tmp, "summary"),
	)
	for k, v := range env {
		environ = append(environ, k+"="+v)
	}

	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Env = environ
	out, err := cmd.CombinedOutput()
	t.Logf("tamci %s:\n%s", strings.Join(args, " "), out)
	return result{
		outputs: parseCommandFile(t, outputFile),
		state:   parseCommandFile(t, stateFile),
		log:     string(out),
		err:     err,
	}
}

// parseCommandFile reads a GITHUB_OUTPUT/GITHUB_STATE file: name=value lines
// and name<<DELIM heredocs.
func parseCommandFile(t *testing.T, path string) map[string]string {
	t.Helper()
	values := map[string]string{}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return values
	}
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	for i := 0; i < len(lines); i++ {
		if name, delim, ok := strings.Cut(lines[i], "<<"); ok && !strings.Contains(name, "=") {
			var body []string
			for i++; i < len(lines) && lines[i] != delim; i++ {
				body = append(body, lines[i])
			}
			values[name] = strings.Join(body, "\n")
			continue
		}
		if name, value, ok := strings.Cut(lines[i], "="); ok {
			values[name] = value
		}
	}
	return values
}
