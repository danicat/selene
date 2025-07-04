package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"log"
	"testing"
	"time"

	// Ensure this path matches your actual module structure for testrunner
	"github.com/your-username/selene/testrunner"
)

// MockMutationCreator provides a mock implementation of the MutationCreator interface.
type MockMutationCreator struct {
	CreateMutationsFunc func(filenames []string, mutationDir string) (string, error)
	CalledFilenames     []string
	CalledMutationDir   string
}

func (mmc *MockMutationCreator) CreateMutations(filenames []string, mutationDir string) (string, error) {
	mmc.CalledFilenames = filenames
	mmc.CalledMutationDir = mutationDir
	if mmc.CreateMutationsFunc != nil {
		return mmc.CreateMutationsFunc(filenames, mutationDir)
	}
	// Default success: return a plausible overlay path
	return filepath.Join(mutationDir, "overlay.json"), nil
}

// MockTestExecutor provides a mock implementation of the TestExecutor interface.
type MockTestExecutor struct {
	RunTestsFunc      func(pkgDir, overlayPath string) ([]testrunner.TestEvent, error)
	CalledPkgDir      string
	CalledOverlayPath string
}

func (mte *MockTestExecutor) RunTests(pkgDir, overlayPath string) ([]testrunner.TestEvent, error) {
	mte.CalledPkgDir = pkgDir
	mte.CalledOverlayPath = overlayPath
	if mte.RunTestsFunc != nil {
		return mte.RunTestsFunc(pkgDir, overlayPath)
	}
	// Default success with no actual test events, leading to "no tests covered the mutations"
	return []testrunner.TestEvent{}, nil
}

// Helper to manage environment variables during tests
func withEnvVar(key, value string, testFunc func()) {
	originalValue, wasSet := os.LookupEnv(key)
	os.Setenv(key, value)
	defer func() {
		if wasSet {
			os.Setenv(key, originalValue)
		} else {
			os.Unsetenv(key)
		}
	}()
	testFunc()
}

func withoutEnvVar(key string, testFunc func()) {
	originalValue, wasSet := os.LookupEnv(key)
	os.Unsetenv(key)
	defer func() {
		if wasSet {
			os.Setenv(key, originalValue)
		}
	}()
	testFunc()
}

func TestRunApp(t *testing.T) {
	defaultArgs := []string{"file.go"}

	// Predefined test event sets
	ts := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
	allMutationsCaughtEvents := []testrunner.TestEvent{
		{Time: ts, Action: "run", Test: "TestOne"},
		{Time: ts, Action: "fail", Test: "TestOne", Elapsed: 0.1}, // fail = caught
	}
	someMutationsMissedEvents := []testrunner.TestEvent{
		{Time: ts, Action: "run", Test: "TestOne"},
		{Time: ts, Action: "fail", Test: "TestOne", Elapsed: 0.1}, // caught
		{Time: ts, Action: "run", Test: "TestTwo"},
		{Time: ts, Action: "pass", Test: "TestTwo", Elapsed: 0.2}, // pass = not caught
	}
	noTestsRunEvents := []testrunner.TestEvent{}

	t.Run("help flag (-h)", func(t *testing.T) {
		var outBuf bytes.Buffer
		err := runApp([]string{"-h"}, &outBuf, nil, nil) // mc and te not used
		if err != nil {
			t.Errorf("runApp with -h returned error: %v", err)
		}
		if !strings.Contains(outBuf.String(), "Usage: selene") {
			t.Errorf("runApp with -h did not show usage. Output: %s", outBuf.String())
		}
	})

	t.Run("no input files", func(t *testing.T) {
		var outBuf bytes.Buffer
		err := runApp([]string{}, &outBuf, nil, nil) // mc and te not used
		if err == nil {
			t.Error("runApp with no files expected error, got nil")
		} else if !strings.Contains(err.Error(), "no input Go files provided") {
			t.Errorf("runApp error message mismatch. Got: %v", err)
		}
		if !strings.Contains(outBuf.String(), "Usage: selene") {
			t.Errorf("runApp with no files did not show usage. Output: %s", outBuf.String())
		}
	})

	t.Run("successful run, all mutations caught", func(t *testing.T) {
		var outBuf bytes.Buffer
		mockMC := &MockMutationCreator{}
		mockTE := &MockTestExecutor{
			RunTestsFunc: func(pkgDir, overlayPath string) ([]testrunner.TestEvent, error) {
				return allMutationsCaughtEvents, nil
			},
		}

		withoutEnvVar("GOMUTATION", func() {
			withoutEnvVar("SELENE_KEEP_MUTATIONS", func() {
				err := runApp(defaultArgs, &outBuf, mockMC, mockTE)
				if err != nil {
					t.Fatalf("runApp failed: %v. Output: %s", err, outBuf.String())
				}
				if !strings.Contains(outBuf.String(), "PASS: All applied mutations were caught by tests.") {
					t.Errorf("Expected PASS message, got: %s", outBuf.String())
				}
				if len(mockMC.CalledFilenames) == 0 || mockMC.CalledFilenames[0] != defaultArgs[0] {
					t.Errorf("MutationCreator.CreateMutations not called with correct file: %v", mockMC.CalledFilenames)
				}
				// Check that mutation dir was a temp one (contains selene-mutation-)
				if !strings.Contains(mockMC.CalledMutationDir, "selene-mutation-") {
					t.Errorf("Expected temp mutation dir, got: %s", mockMC.CalledMutationDir)
				}
			})
		})
	})

	t.Run("user-specified GOMUTATION directory", func(t *testing.T) {
		userDir := t.TempDir() // Create an actual temp dir for the test

		var outBuf bytes.Buffer
		mockMC := &MockMutationCreator{
			CreateMutationsFunc: func(filenames []string, mutationDir string) (string, error) {
				if mutationDir != userDir {
					t.Errorf("Expected GOMUTATION dir '%s', got '%s'", userDir, mutationDir)
				}
				return filepath.Join(mutationDir, "overlay.json"), nil
			},
		}
		mockTE := &MockTestExecutor{
			RunTestsFunc: func(pkgDir, overlayPath string) ([]testrunner.TestEvent, error) {
				return allMutationsCaughtEvents, nil
			},
		}
		withEnvVar("GOMUTATION", userDir, func() {
			err := runApp(defaultArgs, &outBuf, mockMC, mockTE)
			if err != nil {
				t.Fatalf("runApp with GOMUTATION failed: %v. Output: %s", err, outBuf.String())
			}
		})
	})

	t.Run("error from CreateMutations", func(t *testing.T) {
		var outBuf bytes.Buffer
		expectedErr := errors.New("mock CreateMutations error")
		mockMC := &MockMutationCreator{
			CreateMutationsFunc: func(filenames []string, mutationDir string) (string, error) {
				return "", expectedErr
			},
		}
		mockTE := &MockTestExecutor{} // Should not be called

		withoutEnvVar("GOMUTATION", func() {
			err := runApp(defaultArgs, &outBuf, mockMC, mockTE)
			if err == nil {
				t.Fatal("runApp expected error from CreateMutations, got nil")
			}
			if !strings.Contains(err.Error(), expectedErr.Error()) {
				t.Errorf("runApp error mismatch. Got: %v, want to contain: %v", err, expectedErr)
			}
		})
	})

	t.Run("error from RunTests", func(t *testing.T) {
		var outBuf bytes.Buffer
		expectedErr := errors.New("mock RunTests error")
		mockMC := &MockMutationCreator{} // Assumed to succeed
		mockTE := &MockTestExecutor{
			RunTestsFunc: func(pkgDir, overlayPath string) ([]testrunner.TestEvent, error) {
				return nil, expectedErr
			},
		}
		withoutEnvVar("GOMUTATION", func() {
			err := runApp(defaultArgs, &outBuf, mockMC, mockTE)
			if err == nil {
				t.Fatal("runApp expected error from RunTests, got nil")
			}
			if !strings.Contains(err.Error(), expectedErr.Error()) {
				t.Errorf("runApp error mismatch. Got: %v, want to contain: %v", err, expectedErr)
			}
		})
	})

	t.Run("some mutations missed", func(t *testing.T) {
		var outBuf bytes.Buffer
		mockMC := &MockMutationCreator{}
		mockTE := &MockTestExecutor{
			RunTestsFunc: func(pkgDir, overlayPath string) ([]testrunner.TestEvent, error) {
				return someMutationsMissedEvents, nil
			},
		}
		withoutEnvVar("GOMUTATION", func() {
			err := runApp(defaultArgs, &outBuf, mockMC, mockTE)
			if err == nil {
				t.Fatal("runApp expected error when mutations are missed, got nil")
			}
			if !strings.Contains(err.Error(), "mutations were not caught") {
				t.Errorf("Expected error about missed mutations, got: %v", err)
			}
			if !strings.Contains(outBuf.String(), "FAIL:") || !strings.Contains(outBuf.String(), "1 out of 2 mutations were not caught") {
				t.Errorf("Expected FAIL message in output, got: %s", outBuf.String())
			}
		})
	})

	t.Run("no tests cover mutations", func(t *testing.T) {
		var outBuf bytes.Buffer
		mockMC := &MockMutationCreator{}
		mockTE := &MockTestExecutor{
			RunTestsFunc: func(pkgDir, overlayPath string) ([]testrunner.TestEvent, error) {
				return noTestsRunEvents, nil
			},
		}
		withoutEnvVar("GOMUTATION", func() {
			err := runApp(defaultArgs, &outBuf, mockMC, mockTE)
			if err == nil {
				t.Fatal("runApp expected error when no tests cover mutations, got nil")
			}
			if !strings.Contains(err.Error(), "no tests covered the mutations") {
				t.Errorf("Expected error about no tests covering mutations, got: %v", err)
			}
			if !strings.Contains(outBuf.String(), "No tests were run or found that covered the applied mutations.") {
				t.Errorf("Expected message about no tests run, got: %s", outBuf.String())
			}
		})
	})

	t.Run("SELENE_LOG_LEVEL=DEBUG enables logging", func(t *testing.T) {
		var stdOutBuf bytes.Buffer // For normal output

		// Capture os.Stderr for logging
		oldStderr := os.Stderr
		r, w, _ := os.Pipe()
		os.Stderr = w

		mockMC := &MockMutationCreator{}
		mockTE := &MockTestExecutor{
			RunTestsFunc: func(pkgDir, overlayPath string) ([]testrunner.TestEvent, error) {
				log.Println("Test log message during RunTests") // This should go to actual Stderr if log is set
				return allMutationsCaughtEvents, nil
			},
		}

		withEnvVar("SELENE_LOG_LEVEL", "DEBUG", func() {
			withoutEnvVar("GOMUTATION", func() { // Ensure temp dir is used for predictable log messages
				err := runApp(defaultArgs, &stdOutBuf, mockMC, mockTE)
				if err != nil {
					t.Errorf("runApp failed with debug logging: %v", err)
				}
			})
		})

		w.Close()             // Close writer to finish capturing
		os.Stderr = oldStderr // Restore original stderr

		var capturedLogOutput bytes.Buffer
		io.Copy(&capturedLogOutput, r)

		logOutput := capturedLogOutput.String()
		if !strings.Contains(logOutput, "Test log message during RunTests") {
			t.Errorf("Expected test log message in Stderr with DEBUG level. Got: %s", logOutput)
		}
		if !strings.Contains(logOutput, "Created temporary mutation directory") {
			t.Errorf("Expected 'Created temporary mutation directory' log. Got: %s", logOutput)
		}
	})

	t.Run("SELENE_KEEP_MUTATIONS keeps temp dir", func(t *testing.T) {
		// This test is more conceptual for runApp as cleanup is deferred.
		// The actual check of os.RemoveAll not being called is hard without deeper os mocking.
		// We rely on the logic path in runApp: if SELENE_KEEP_MUTATIONS is set, os.RemoveAll isn't called.
		// We can check that the log message indicates it's being kept.
		var stdOutBuf bytes.Buffer
		oldStderr := os.Stderr
		r, w, _ := os.Pipe()
		os.Stderr = w // Capture log output

		mockMC := &MockMutationCreator{}
		mockTE := &MockTestExecutor{RunTestsFunc: func(pkgDir, overlayPath string) ([]testrunner.TestEvent, error) {
			return allMutationsCaughtEvents, nil
		}}

		withEnvVar("SELENE_LOG_LEVEL", "DEBUG", func() { // Enable logging to see the message
			withEnvVar("SELENE_KEEP_MUTATIONS", "true", func() {
				withoutEnvVar("GOMUTATION", func() { // Ensure temp dir is used
					// We need to run this in a goroutine to allow the defer to execute before test ends,
					// or find another way to inspect the state before/after potential cleanup.
					// For simplicity, we'll check the log message which is set before defer.
					// A full test of defer requires more complex setup or refactoring runApp's cleanup.
					runApp(defaultArgs, &stdOutBuf, mockMC, mockTE) // Error ignored for this specific check
				})
			})
		})

		w.Close()
		os.Stderr = oldStderr
		var logCap bytes.Buffer
		io.Copy(&logCap, r)

		if !strings.Contains(logCap.String(), "Keeping temporary mutation directory (SELENE_KEEP_MUTATIONS is set)") {
			t.Errorf("Expected log message about keeping temp dir. Got: %s", logCap.String())
		}
	})
}
