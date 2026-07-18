//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// binPath is set by TestMain to the freshly built netcheckout binary.
var binPath string

func TestMain(m *testing.M) {
	os.Exit(runMain(m))
}

// runMain builds netcheckout to a temp dir, runs the suite, and cleans up. A build
// failure prints the compiler output and skips m.Run entirely (exit 1).
func runMain(m *testing.M) int {
	tmpDir, err := os.MkdirTemp("", "netcheckout-e2e-bin-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "e2e: create temp dir:", err)
		return 1
	}
	defer os.RemoveAll(tmpDir)

	binPath = filepath.Join(tmpDir, "netcheckout")
	build := exec.Command("go", "build", "-o", binPath, "github.com/andresbott/netcheckout")
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "e2e: build netcheckout:", err)
		return 1
	}

	return m.Run()
}

func TestBinaryBuildsAndRuns(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "unused-config.yaml")
	stdout, _, exitCode := runCLI(t, configPath, "version")
	if exitCode != 0 {
		t.Fatalf("version exit = %d, want 0", exitCode)
	}
	if !strings.Contains(stdout, "Version:") {
		t.Fatalf("version stdout = %q, want it to contain %q", stdout, "Version:")
	}
}
