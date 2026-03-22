package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/danicat/selene/internal/mutator"

	"github.com/danicat/selene/internal/runner"
)

const GOMUTATION = "GOMUTATION"

func usage() {
	fmt.Println("Usage:\nselene [flags] file.go [file2.go ...]")
	flag.PrintDefaults()
}

func main() {
	var verbose bool
	var mutationDir string
	var workers int
	var seed int64
	var shuffle bool
	var timeout time.Duration
	var jsonOut bool

	flag.BoolVar(&verbose, "v", false, "Enable verbose output")
	flag.StringVar(&mutationDir, "output", "", "Directory to store mutated files (default: temporary dir)")
	flag.IntVar(&workers, "workers", 0, "Number of parallel workers (default: NumCPU)")
	flag.Int64Var(&seed, "seed", 0, "Seed for randomization (default: random)")
	flag.BoolVar(&shuffle, "shuffle", false, "Enable randomization of file processing order")
	flag.DurationVar(&timeout, "timeout", 10*time.Second, "Maximum time allowed for a single test run")
	flag.BoolVar(&jsonOut, "json", false, "Output results in JSON format")
	flag.Parse()

	if !verbose && !jsonOut {
		log.SetOutput(io.Discard)
	}

	if flag.NArg() < 1 {
		usage()
		os.Exit(1)
	}

	// Setup mutation directory
	if mutationDir == "" {
		mutationDir = os.Getenv(GOMUTATION)
	}
	if mutationDir == "" {
		tmpDir, err := os.MkdirTemp("", "mutation")
		if err != nil {
			log.Fatalln(err)
		}
		mutationDir = tmpDir
	}
	err := os.MkdirAll(mutationDir, os.ModePerm)
	if err != nil {
		log.Fatalf("failed to create mutation directory: %s", err)
	}
	if verbose {
		log.Printf("mutation directory: %s", mutationDir)
	}

	patterns := flag.Args()

	// Register all available mutators (UX: enable all by default)
	mutators := []mutator.Mutator{
		&mutator.ReverseIfCond{},
		&mutator.ArithmeticMutator{},
		&mutator.ComparisonMutator{},
		&mutator.BooleanMutator{},
		&mutator.ConditionalsBoundaryMutator{},
		&mutator.IncrementDecrementMutator{},
	}

	config := runner.Config{
		Verbose:     verbose,
		MutationDir: mutationDir,
		Mutators:    mutators,
		Workers:     workers,
		Seed:        seed,
		Shuffle:     shuffle,
		Timeout:     timeout,
	}

	report, err := runner.Run(patterns, config)

	if err != nil {
		log.Fatalf("error running mutations: %s", err)
	}

	if report.Total == 0 {
		fmt.Println("No mutations found.")
		os.Exit(0)
	}

	// Calculate stats per test
	allTests := make(map[string]bool)
	goodTests := []string{}
	badTests := []string{}

	// Extract all unique test names from test data packages
	pkgs := make(map[string]bool)
	for _, f := range patterns {
		// handle patterns like ./...
		if strings.Contains(f, "...") {
			out, err := exec.Command("go", "list", f).Output()
			if err == nil {
				for _, p := range strings.Split(strings.TrimSpace(string(out)), "\n") {
					pkgs[p] = true
				}
			}
		} else {
			// handle file paths
			pkgs[filepath.Dir(f)] = true
		}
	}

	for pkg := range pkgs {
		out, err := exec.Command("go", "test", "-list", ".", pkg).Output()
		if err == nil {
			lines := strings.Split(string(out), "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "Test") {
					allTests[line] = true
				}
			}
		}
	}

	// Ensure we include any test that killed something even if -list missed it
	for test := range report.TestKills {
		allTests[test] = true
	}

	for test := range allTests {
		if len(report.TestKills[test]) > 0 {
			goodTests = append(goodTests, test)
		} else {
			badTests = append(badTests, test)
		}
	}

	testQualityScore := 0.0
	if len(allTests) > 0 {
		testQualityScore = float64(len(goodTests)) / float64(len(allTests)) * 100
	}

	if jsonOut {
		type Output struct {
			TotalMutations   int                 `json:"total_mutations"`
			Killed           int                 `json:"killed"`
			Survived         int                 `json:"survived"`
			Timeouts         int                 `json:"timeouts"`
			Uncovered        int                 `json:"uncovered"`
			TotalTests       int                 `json:"total_tests"`
			GoodTests        []string            `json:"good_tests"`
			BadTests         []string            `json:"bad_tests"`
			MutationScore    float64             `json:"mutation_score"`
			TestQualityScore float64             `json:"test_quality_score"`
			TestKills        map[string][]string `json:"test_kills,omitempty"`
		}
		out := Output{
			TotalMutations:   report.Total,
			Killed:           report.Killed,
			Survived:         report.Survived,
			Timeouts:         report.Timeouts,
			Uncovered:        report.Uncovered,
			TotalTests:       len(allTests),
			GoodTests:        goodTests,
			BadTests:         badTests,
			MutationScore:    report.Score(),
			TestQualityScore: testQualityScore,
		}
		if verbose {
			out.TestKills = report.TestKills
		}
		bytes, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(bytes))
	} else {
		// Final Report (UX: Match legacy reporting format)
		fmt.Printf("\nTotal mutations: %d\n", report.Total)
		fmt.Printf("Killed:          %d\n", report.Killed)
		fmt.Printf("Timeouts:        %d\n", report.Timeouts)
		fmt.Printf("Survived:        %d\n", report.Survived)
		fmt.Printf("Uncovered:       %d\n", report.Uncovered)
		if report.BuildFailures > 0 {
			fmt.Printf("Build Failures:  %d\n", report.BuildFailures)
		}

		fmt.Printf("\nTotal tests:     %d\n", len(allTests))
		fmt.Printf("Good tests:      %d\n", len(goodTests))
		fmt.Printf("Bad tests:       %d\n", len(badTests))

		if len(badTests) > 0 {
			fmt.Println("\nBad tests (caught 0 mutations):")
			for _, test := range badTests {
				fmt.Printf("- %s\n", test)
			}
		}

		if verbose && len(goodTests) > 0 {
			fmt.Println("\nGood tests details:")
			for _, test := range goodTests {
				fmt.Printf("- %s (caught %d mutations): %s\n", test, len(report.TestKills[test]), strings.Join(report.TestKills[test], ", "))
			}
		}
		fmt.Printf("\nMutation Score:     %.2f%% (killed/total mutations)\n", report.Score())
				fmt.Printf("Test Quality Score: %.2f%% (good tests/total tests)\n", testQualityScore)
	}
}

