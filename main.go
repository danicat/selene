package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/your-username/selene/mutator"
	"github.com/your-username/selene/testrunner"
)

const GOMUTATION = "GOMUTATION" // Environment variable for specifying mutation directory

// MutationCreator defines the interface for creating mutations.
type MutationCreator interface {
	CreateMutations(filenames []string, mutationDir string) (string, error)
}

// TestExecutor defines the interface for running tests.
type TestExecutor interface {
	RunTests(pkgDir, overlayPath string) ([]testrunner.TestEvent, error)
}

func usage(out io.Writer) {
	fmt.Fprintln(out, "Usage: selene <file.go> [file2.go ...]")
	fmt.Fprintln(out, "\nEnvironment Variables:")
	fmt.Fprintln(out, "  GOMUTATION: Specify a directory to store mutation files.")
	fmt.Fprintln(out, "                If not set, a temporary directory will be created and removed.")
	fmt.Fprintln(out, "  SELENE_KEEP_MUTATIONS: Set to any value to prevent deletion of the temporary mutation directory.")
	fmt.Fprintln(out, "  SELENE_LOG_LEVEL: Set to DEBUG to enable verbose logging to stderr.")
}

// runApp encapsulates the core logic of the application.
// It returns an error if the process should exit with a non-zero status, nil otherwise.
func runApp(args []string, out io.Writer, mc MutationCreator, te TestExecutor) error {
	if len(args) == 0 {
		usage(out)
		return fmt.Errorf("no input Go files provided")
	}
	inputFilenames := args

	if inputFilenames[0] == "-h" || inputFilenames[0] == "--help" {
		usage(out)
		return nil // Successful exit for help
	}

	if os.Getenv("SELENE_LOG_LEVEL") == "DEBUG" {
		log.SetOutput(os.Stderr)
	} else {
		log.SetOutput(io.Discard)
	}

	var actualMutationDir string
	var tempDirCleanup func()

	userSpecifiedMutationDir := os.Getenv(GOMUTATION)
	if userSpecifiedMutationDir != "" {
		actualMutationDir = userSpecifiedMutationDir
		log.Printf("Using user-specified mutation directory: %s", actualMutationDir)
		err := os.MkdirAll(actualMutationDir, 0755)
		if err != nil {
			return fmt.Errorf("failed to create or access user-specified mutation directory %s: %w", actualMutationDir, err)
		}
	} else {
		tmpDir, err := os.MkdirTemp("", "selene-mutation-")
		if err != nil {
			return fmt.Errorf("error creating temp directory: %w", err)
		}
		actualMutationDir = tmpDir
		log.Printf("Created temporary mutation directory: %s", actualMutationDir)
		tempDirCleanup = func() {
			if os.Getenv("SELENE_KEEP_MUTATIONS") == "" {
				log.Printf("Removing temporary mutation directory: %s", tmpDir)
				os.RemoveAll(tmpDir)
			} else {
				log.Printf("Keeping temporary mutation directory (SELENE_KEEP_MUTATIONS is set): %s", tmpDir)
			}
		}
		defer func() {
			if r := recover(); r != nil {
				fmt.Fprintf(out, "Panic occurred: %v. Temporary mutation dir kept at: %s\n", r, tmpDir)
			} else if tempDirCleanup != nil {
				tempDirCleanup()
			}
		}()
	}

	overlayPath, err := mc.CreateMutations(inputFilenames, actualMutationDir)
	if err != nil {
		return fmt.Errorf("error creating mutations: %w", err)
	}
	log.Printf("Overlay file created at: %s", overlayPath)

	absPath, err := filepath.Abs(inputFilenames[0])
	if err != nil {
		return fmt.Errorf("error getting absolute path for %s: %w", inputFilenames[0], err)
	}
	pkgDir := filepath.Dir(absPath)
	log.Printf("Running go test on package directory: %s", pkgDir)

	testEvents, err := te.RunTests(pkgDir, overlayPath)
	if err != nil {
		return fmt.Errorf("error running tests: %w", err)
	}

	return processTestResults(testEvents, out)
}

func main() {
	// Initialize real components
	realFS := mutator.RealFileSystem{}
	realFCP := mutator.RealFileContentProvider{}
	mut := mutator.New(realFS, realFCP) // This is a mutator.Mutator

	realCmdRunner := testrunner.RealCommandRunner{}
	tr := testrunner.New(realCmdRunner) // This is a testrunner.TestRunner

	// os.Args[0] is the program name, os.Args[1:] are the actual arguments.
	// Pass the real implementations that satisfy the interfaces.
	err := runApp(os.Args[1:], os.Stdout, mut, tr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// processTestResults processes the events and prints results to out.
// It returns an error if mutations were not caught, nil otherwise.
func processTestResults(events []testrunner.TestEvent, out io.Writer) error {
	testCount := 0
	caughtMutations := 0

	for _, event := range events {
		if event.Test == "" { // Skip non-test events (e.g., package pass/fail summaries)
			continue
		}

		action := event.Action
		// Optionally print detailed test event line
		// fmt.Fprintf(out, "Test: %s, Action: %s, Elapsed: %.2fs\n", event.Test, event.Action, event.Elapsed)

		switch action {
		case "pass": // Test passed with mutation active => mutation NOT caught
			testCount++
			fmt.Fprintf(out, "  --- UNCAUGHT: %s (%0.2fs)\n", event.Test, event.Elapsed)
		case "fail": // Test failed with mutation active => mutation CAUGHT
			testCount++
			caughtMutations++
			fmt.Fprintf(out, "  --- CAUGHT:   %s (%0.2fs)\n", event.Test, event.Elapsed)
		case "skip":
			// testCount++ // Decide if skipped tests should count towards total
			fmt.Fprintf(out, "  --- SKIPPED:  %s (%0.2fs)\n", event.Test, event.Elapsed)
		}
	}

	if testCount == 0 {
		fmt.Fprintln(out, "\nNo tests were run or found that covered the applied mutations.")
		// This state might be considered a failure in mutation testing context,
		// as it means the mutations are not validated.
		return fmt.Errorf("no tests covered the mutations")
	}

	mutationScore := 0.0
	if testCount > 0 {
		mutationScore = (float64(caughtMutations) / float64(testCount)) * 100
	}

	fmt.Fprintf(out, "\nMutation Score: %.2f%% (%d out of %d mutations caught by tests)\n", mutationScore, caughtMutations, testCount)

	if caughtMutations < testCount {
		// This is the primary failure condition for the tool's purpose.
		return fmt.Errorf("%d out of %d mutations were not caught by any test", testCount-caughtMutations, testCount)
	}

	fmt.Fprintln(out, "PASS: All applied mutations were caught by tests.")
	return nil
}
