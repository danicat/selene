package testrunner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strings"
	"time"
)

// CommandRunner defines an interface for running external commands.
type CommandRunner interface {
	Run(command string, args ...string) (stdout []byte, stderr []byte, err error)
}

// RealCommandRunner implements CommandRunner using os/exec.
type RealCommandRunner struct{}

// Run executes the given command with arguments and returns its stdout, stderr, and any error.
func (r RealCommandRunner) Run(command string, args ...string) ([]byte, []byte, error) {
	cmd := exec.Command(command, args...)
	var outb, errb bytes.Buffer
	cmd.Stdout = &outb
	cmd.Stderr = &errb
	err := cmd.Run() // err can be *exec.ExitError if command runs and exits non-zero
	return outb.Bytes(), errb.Bytes(), err
}

// TestEvent represents a single event from `go test -json` output.
type TestEvent struct {
	Time    time.Time `json:"Time"`
	Action  string    `json:"Action"`
	Package string    `json:"Package"`
	Test    string    `json:"Test,omitempty"`    // Test name, empty for package-level events
	Elapsed float64   `json:"Elapsed,omitempty"` // Seconds
	Output  string    `json:"Output,omitempty"`
}

// TestRunner executes Go tests and parses their output.
type TestRunner struct {
	cmdRunner CommandRunner
}

// New creates a new TestRunner with the given CommandRunner.
func New(cmdRunner CommandRunner) *TestRunner {
	return &TestRunner{cmdRunner: cmdRunner}
}

// RunTests executes `go test --json` for the specified package directory using the overlay file.
// It returns a slice of TestEvents or an error if the test execution or parsing fails.
func (tr *TestRunner) RunTests(pkgDir, overlayPath string) ([]TestEvent, error) {
	args := []string{"test", "-json"}
	if overlayPath != "" {
		args = append(args, "-overlay="+overlayPath)
	}
	args = append(args, pkgDir) // Add pkgDir as the target for tests

	log.Printf("Executing command: go %s", strings.Join(args, " "))
	stdout, stderr, err := tr.cmdRunner.Run("go", args...)

	// `go test` exits with 1 if tests fail, which is expected in mutation testing (mutation caught)
	// or if tests pass (mutation not caught).
	// It exits with a different code for build failures or other critical errors.
	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			// Not an ExitError, could be path error, command not found, etc.
			log.Printf("Failed to execute 'go test': %v. Stderr: %s", err, string(stderr))
			return nil, fmt.Errorf("failed to run 'go test': %w. Stderr: %s", err, string(stderr))
		}
		// ExitError means the command ran but exited non-zero.
		// For `go test`, exit code 1 typically means test failures.
		// Exit code 0 means tests passed.
		// Other codes (like 2) mean issues with packages, flags, etc.
		if exitErr.ExitCode() != 1 && exitErr.ExitCode() != 0 {
			log.Printf("'go test' command exited with code %d. Stderr: %s. Stdout: %s", exitErr.ExitCode(), string(stderr), string(stdout))
			return nil, fmt.Errorf("'go test' failed with exit code %d: %s. Output: %s", exitErr.ExitCode(), string(stderr), string(stdout))
		}
		// If exit code is 1 (test failures) or 0 (tests passed), we proceed to parse output.
		log.Printf("'go test' exited with code %d (expected for test completion). Stderr: %s", exitErr.ExitCode(), string(stderr))
	}

	if len(stderr) > 0 {
		// Output from `go test` stderr might contain useful info even on "successful" runs (e.g., race detector warnings)
		log.Printf("go test stderr:\n%s", string(stderr))
	}

	return parseGoTestOutput(stdout)
}

// parseGoTestOutput parses the concatenated JSON output from `go test --json`.
func parseGoTestOutput(output []byte) ([]TestEvent, error) {
	var events []TestEvent
	if len(output) == 0 {
		return events, nil // No output to parse
	}

	decoder := json.NewDecoder(bytes.NewReader(output))
	for {
		var event TestEvent
		if err := decoder.Decode(&event); err == io.EOF {
			break // End of output
		} else if err != nil {
			// Log the problematic part of the output if possible
			log.Printf("Error decoding test event JSON: %v. Output snippet: %s", err, firstNBytes(output, 200))
			return nil, fmt.Errorf("error unmarshaling test event: %w. Input: %s", err, firstNBytes(output, 200))
		}
		events = append(events, event)
	}
	return events, nil
}

// firstNBytes returns the first N bytes of a byte slice as a string for logging.
func firstNBytes(b []byte, n int) string {
	if len(b) == 0 {
		return ""
	}
	if len(b) > n {
		return string(b[:n]) + "..."
	}
	return string(b)
}
