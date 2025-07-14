package testrunner

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"
)

// MockCommandRunner is a mock implementation of the CommandRunner interface.
type MockCommandRunner struct {
	RunFunc func(command string, args ...string) (stdout []byte, stderr []byte, err error)
	// Store details of the last call
	CalledCommand string
	CalledArgs    []string
}

// Run executes the mock command.
func (mcr *MockCommandRunner) Run(command string, args ...string) ([]byte, []byte, error) {
	mcr.CalledCommand = command
	mcr.CalledArgs = args
	if mcr.RunFunc != nil {
		return mcr.RunFunc(command, args...)
	}
	return nil, nil, fmt.Errorf("RunFunc not implemented in MockCommandRunner")
}

func TestParseGoTestOutput(t *testing.T) {
	// Helper to create JSON output for a TestEvent
	makeEventJSON := func(event TestEvent) string {
		b, _ := json.Marshal(event)
		return string(b)
	}

	time1, _ := time.Parse(time.RFC3339Nano, "2023-01-15T14:30:00.123Z")
	event1 := TestEvent{Time: time1, Action: "run", Package: "pkg", Test: "TestOne"}
	event2 := TestEvent{Time: time1.Add(time.Second), Action: "pass", Package: "pkg", Test: "TestOne", Elapsed: 0.5}
	event3 := TestEvent{Time: time1.Add(2 * time.Second), Action: "fail", Package: "pkg", Test: "TestTwo", Elapsed: 0.2, Output: "some error"}

	tests := []struct {
		name          string
		input         []byte
		want          []TestEvent
		expectError   bool
		errorContains string
	}{
		{
			name:  "single valid event",
			input: []byte(makeEventJSON(event1) + "\n"),
			want:  []TestEvent{event1},
		},
		{
			name:  "multiple valid events",
			input: []byte(makeEventJSON(event1) + "\n" + makeEventJSON(event2) + "\n" + makeEventJSON(event3) + "\n"),
			want:  []TestEvent{event1, event2, event3},
		},
		{
			name:  "empty input",
			input: []byte(""),
			want:  []TestEvent{},
		},
		{
			name:  "input with only newline",
			input: []byte("\n"),
			want:  []TestEvent{},
		},
		{
			name:          "malformed JSON",
			input:         []byte(`{"Time":"2023-01-15T14:30:00.123Z", "Action":"run", "Package":"pkg", "Test":TestOne}` + "\n"), // TestOne not quoted
			want:          nil,
			expectError:   true,
			errorContains: "invalid character 'T' looking for beginning of value",
		},
		{
			name:          "stream with one malformed JSON among valid ones",
			input:         []byte(makeEventJSON(event1) + "\n" + `{"Action":"bad` + "\n" + makeEventJSON(event3) + "\n"),
			want:          nil, // Parsing stops at first error
			expectError:   true,
			errorContains: "unexpected end of JSON input",
		},
		{
			name:  "valid JSON but not a TestEvent structure",
			input: []byte(`{"SomeOtherField": "value"}` + "\n"),
			// This will parse successfully into a TestEvent with zero values for missing fields.
			// The json unmarshaler is lenient with extra/missing fields.
			want: []TestEvent{{}},
		},
		{
			name:  "JSON with null values for omitempty fields",
			input: []byte(`{"Time":"2023-01-15T14:30:00.123Z", "Action":"output", "Package":"pkg", "Test":null, "Elapsed":null, "Output":"log\n"}` + "\n"),
			want:  []TestEvent{{Time: time1, Action: "output", Package: "pkg", Output: "log\n"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseGoTestOutput(tt.input)

			if tt.expectError {
				if err == nil {
					t.Fatalf("parseGoTestOutput() expected error, got nil")
				}
				if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("parseGoTestOutput() error = %v, want error containing %q", err, tt.errorContains)
				}
			} else {
				if err != nil {
					t.Fatalf("parseGoTestOutput() unexpected error: %v", err)
				}
				if !reflect.DeepEqual(got, tt.want) {
					// For detailed diff if complex structs are involved:
					// gotJSON, _ := json.MarshalIndent(got, "", "  ")
					// wantJSON, _ := json.MarshalIndent(tt.want, "", "  ")
					// t.Errorf("parseGoTestOutput() got = \n%s\nwant = \n%s", string(gotJSON), string(wantJSON))
					t.Errorf("parseGoTestOutput() got = %#v, want %#v", got, tt.want)
				}
			}
		})
	}
}

func TestTestRunner_RunTests(t *testing.T) {
	pkgDir := "./testpkg"
	overlayPath := "/tmp/overlay.json"

	time1, _ := time.Parse(time.RFC3339Nano, "2023-01-15T15:00:00Z")
	event1 := TestEvent{Time: time1, Action: "run", Package: pkgDir, Test: "TestA"}
	event2 := TestEvent{Time: time1.Add(time.Millisecond * 100), Action: "pass", Package: pkgDir, Test: "TestA", Elapsed: 0.1}
	eventJSONStream := func() []byte {
		e1, _ := json.Marshal(event1)
		e2, _ := json.Marshal(event2)
		return []byte(string(e1) + "\n" + string(e2) + "\n")
	}

	t.Run("successful test execution - tests pass (exit 0)", func(t *testing.T) {
		mockCmdRunner := &MockCommandRunner{}
		mockCmdRunner.RunFunc = func(command string, args ...string) ([]byte, []byte, error) {
			return eventJSONStream(), nil, nil // Exit 0
		}
		tr := New(mockCmdRunner)

		events, err := tr.RunTests(pkgDir, overlayPath)
		if err != nil {
			t.Fatalf("RunTests() unexpected error: %v", err)
		}
		if mockCmdRunner.CalledCommand != "go" {
			t.Errorf("Expected command 'go', got '%s'", mockCmdRunner.CalledCommand)
		}
		expectedArgs := []string{"test", "-json", "-overlay=" + overlayPath, pkgDir}
		if !reflect.DeepEqual(mockCmdRunner.CalledArgs, expectedArgs) {
			t.Errorf("RunTests() called with args = %v, want %v", mockCmdRunner.CalledArgs, expectedArgs)
		}
		if len(events) != 2 || !reflect.DeepEqual(events[0], event1) || !reflect.DeepEqual(events[1], event2) {
			t.Errorf("RunTests() got events = %#v, want %#v", events, []TestEvent{event1, event2})
		}
	})

	t.Run("successful test execution - tests fail (exit 1)", func(t *testing.T) {
		mockCmdRunner := &MockCommandRunner{}
		mockCmdRunner.RunFunc = func(command string, args ...string) ([]byte, []byte, error) {
			// Simulate `go test` exiting with 1 due to test failures
			return eventJSONStream(), []byte("FAIL: TestB blah blah"), &exec.ExitError{Stderr: []byte("exit status 1")}
		}
		// Correctly set ExitError.Sys via a ProcessState for ExitCode() to work.
		// This is a bit involved to mock perfectly. A simpler ExitError is often sufficient for basic checks.
		// For this test, we'll use a basic ExitError and rely on the logic that non-nil error with specific codes is handled.
		// The important part is that RunTests proceeds if err is ExitError code 0 or 1.

		// Simplified exit error for testing the logic path
		simulatedExitError := exec.Command("false").Run().(*exec.ExitError)

		mockCmdRunner.RunFunc = func(command string, args ...string) ([]byte, []byte, error) {
			return eventJSONStream(), []byte("FAIL: TestB blah blah"), simulatedExitError
		}

		tr := New(mockCmdRunner)
		events, err := tr.RunTests(pkgDir, overlayPath)
		if err != nil {
			t.Fatalf("RunTests() unexpected error for exit code 1: %v", err)
		}
		if len(events) != 2 {
			t.Errorf("RunTests() got %d events, want 2 for exit code 1", len(events))
		}
	})

	t.Run("go test command fails with non-0/1 exit code", func(t *testing.T) {
		mockCmdRunner := &MockCommandRunner{}
		// Simulate `go test` failing due to a build error or invalid flag (e.g., exit code 2)
		simulatedExitError := &exec.ExitError{Stderr: []byte("exit status 2")}
		// To make ExitCode() work as expected, we need a ProcessState.
		// This is tricky to construct manually. For testing, we can use a command that exits non-zero.
		// Example: run `git` with an invalid command to get an ExitError.
		cmd := exec.Command("go", "tool", "vet", "nonexistentpackage")     // This will produce exit code 2
		_ = cmd.Run()                                                      // We expect this to fail
		actualExitError, ok := cmd.Run().(*exec.ExitError)                 // Run again to get the error
		if !ok && cmd.ProcessState != nil && !cmd.ProcessState.Success() { // Fallback if second run is weird
			actualExitError = &exec.ExitError{ProcessState: cmd.ProcessState}
		} else if !ok {
			t.Log("Warning: Could not reliably produce a sample exec.ExitError with specific code for test setup. Using simplified one.")
			actualExitError = simulatedExitError // Fallback
		}

		mockCmdRunner.RunFunc = func(command string, args ...string) ([]byte, []byte, error) {
			return nil, []byte("Error: build failed."), actualExitError
		}
		tr := New(mockCmdRunner)

		_, err := tr.RunTests(pkgDir, overlayPath)
		if err == nil {
			t.Fatal("RunTests() expected error for non-0/1 exit code, got nil")
		}
		if !strings.Contains(err.Error(), "failed with exit code") {
			t.Errorf("RunTests() error = %v, want error containing 'failed with exit code'", err)
		}
		// Check if the exit code from the error message is what we expect (might be fragile)
		if !strings.Contains(err.Error(), fmt.Sprintf("exit code %d", actualExitError.ExitCode())) {
			t.Errorf("RunTests() error = %v, want error containing specific exit code %d", err, actualExitError.ExitCode())
		}
	})

	t.Run("go test command not found or other execution error", func(t *testing.T) {
		mockCmdRunner := &MockCommandRunner{}
		expectedErr := errors.New("mock command execution failed (e.g., command not found)")
		mockCmdRunner.RunFunc = func(command string, args ...string) ([]byte, []byte, error) {
			return nil, nil, expectedErr
		}
		tr := New(mockCmdRunner)

		_, err := tr.RunTests(pkgDir, overlayPath)
		if err == nil {
			t.Fatal("RunTests() expected error for command execution failure, got nil")
		}
		if !strings.Contains(err.Error(), "failed to run 'go test'") || !strings.Contains(err.Error(), expectedErr.Error()) {
			t.Errorf("RunTests() error = %v, want error containing 'failed to run 'go test'' and '%s'", err, expectedErr.Error())
		}
	})

	t.Run("parsing stdout fails", func(t *testing.T) {
		mockCmdRunner := &MockCommandRunner{}
		mockCmdRunner.RunFunc = func(command string, args ...string) ([]byte, []byte, error) {
			return []byte("this is not json\n"), nil, nil
		}
		tr := New(mockCmdRunner)

		_, err := tr.RunTests(pkgDir, overlayPath)
		if err == nil {
			t.Fatal("RunTests() expected error for JSON parsing failure, got nil")
		}
		if !strings.Contains(err.Error(), "error unmarshaling test event") {
			t.Errorf("RunTests() error = %v, want error containing 'error unmarshaling test event'", err)
		}
	})

	t.Run("run tests with no overlay path", func(t *testing.T) {
		mockCmdRunner := &MockCommandRunner{}
		mockCmdRunner.RunFunc = func(command string, args ...string) ([]byte, []byte, error) {
			return eventJSONStream(), nil, nil
		}
		tr := New(mockCmdRunner)

		_, err := tr.RunTests(pkgDir, "") // Empty overlayPath
		if err != nil {
			t.Fatalf("RunTests() with empty overlayPath failed: %v", err)
		}

		expectedArgs := []string{"test", "-json", pkgDir} // No -overlay flag
		if !reflect.DeepEqual(mockCmdRunner.CalledArgs, expectedArgs) {
			t.Errorf("RunTests() with empty overlayPath, got args = %v, want %v", mockCmdRunner.CalledArgs, expectedArgs)
		}
	})
}

func TestRealCommandRunner(t *testing.T) {
	// This is more of an integration test for RealCommandRunner itself.
	// It relies on the 'go' command being available.
	rco := RealCommandRunner{}

	t.Run("valid command", func(t *testing.T) {
		// `go help test` is a simple command that should succeed and produce output.
		stdout, stderr, err := rco.Run("go", "help", "test")
		if err != nil {
			t.Fatalf("RealCommandRunner.Run(go help test) failed: %v, stderr: %s", err, string(stderr))
		}
		if len(stdout) == 0 {
			t.Error("RealCommandRunner.Run(go help test) stdout should not be empty")
		}
		// Depending on Go version and environment, stderr might have some noise, or be empty.
		// So, not strictly checking stderr for emptiness.
	})

	t.Run("command fails with exit error", func(t *testing.T) {
		// `go run non_existent_file.go` should fail with an ExitError.
		_, stderr, err := rco.Run("go", "run", "non_existent_file_for_test.go")
		if err == nil {
			t.Fatal("RealCommandRunner.Run(go run non_existent_file.go) expected an error, got nil")
		}
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("RealCommandRunner.Run(go run non_existent_file.go) expected *exec.ExitError, got %T", err)
		}
		if exitErr.ExitCode() == 0 {
			t.Errorf("RealCommandRunner.Run(go run non_existent_file.go) expected non-zero exit code, got 0")
		}
		if len(stderr) == 0 {
			t.Error("RealCommandRunner.Run(go run non_existent_file.go) stderr should not be empty for a failed command")
		}
	})

	t.Run("command not found", func(t *testing.T) {
		// Using a very unlikely command name.
		_, _, err := rco.Run("aCommandThatSurelyDoesNotExistOnAnySystem")
		if err == nil {
			t.Fatal("RealCommandRunner.Run(nonexistent_command) expected an error, got nil")
		}
		// Error type might be *exec.Error with Err field os.ErrNotExist or similar,
		// or could be *exec.ExitError on some systems if a shell wrapper indicates "command not found".
		// Check that it's an error related to finding/executing the command.
		if !strings.Contains(err.Error(), "executable file not found") && !strings.Contains(err.Error(), "no such file or directory") {
			t.Logf("Note: On some systems, 'command not found' might manifest differently. Got error: %v (type %T)", err, err)
		}
	})
}
