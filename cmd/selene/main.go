package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/danicat/selene/internal/runner"
)

var version = "dev"

const GOMUTATION = "GOMUTATION"

func usage() {
	fmt.Printf("Selene %s - Fast, Database-Driven Mutation Testing for Go\n\n", version)
	fmt.Println("Usage:\n  selene [flags] file.go [file2.go ...]")
	fmt.Println("\nFlags:")
	flag.PrintDefaults()
}

func main() {
	var showVersion bool
	var verbose bool
	var mutationDir string
	var workers int
	var seed int64
	var shuffle bool
	var timeout time.Duration
	var jsonOut bool
	var dbPath string
	var targeted bool
	var adaptiveTimeout bool

	flag.BoolVar(&showVersion, "version", false, "Print version and exit")
	flag.BoolVar(&verbose, "v", false, "Enable verbose output")
	flag.StringVar(&mutationDir, "output", "", "Directory to store mutated files (default: temporary dir)")
	flag.IntVar(&workers, "workers", 0, "Number of parallel workers (default: NumCPU)")
	flag.Int64Var(&seed, "seed", 0, "Seed for randomization (default: random)")
	flag.BoolVar(&shuffle, "shuffle", false, "Enable randomization of file processing order")
	flag.DurationVar(&timeout, "timeout", 10*time.Second, "Maximum time allowed for a single test run")
	flag.BoolVar(&jsonOut, "json", false, "Output results in JSON format")
	flag.StringVar(&dbPath, "db", "", "Path to SQLite database to store mutation outcomes and test metrics")
	flag.BoolVar(&targeted, "targeted", false, "Enable targeted test execution using coverage index")
	flag.BoolVar(&adaptiveTimeout, "adaptive-timeout", false, "Enable adaptive dynamic timeouts based on baseline test durations")
	flag.Parse()

	if showVersion {
		fmt.Printf("selene %s\n", version)
		return
	}

	if !verbose {
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
		defer func() { _ = os.RemoveAll(tmpDir) }()
	}
	if err := os.MkdirAll(mutationDir, os.ModePerm); err != nil {
		log.Fatalf("failed to create mutation directory: %s", err)
	}
	if verbose {
		log.Printf("mutation directory: %s", mutationDir)
	}

	patterns := flag.Args()

	isTargeted := targeted
	if dbPath != "" {
		isTargeted = true
	}

	config := runner.Config{
		Verbose:         verbose,
		MutationDir:     mutationDir,
		Mutators:        runner.DefaultMutators(),
		Workers:         workers,
		Seed:            seed,
		Shuffle:         shuffle,
		Timeout:         timeout,
		DBPath:          dbPath,
		Targeted:        isTargeted,
		AdaptiveTimeout: adaptiveTimeout,
	}

	report, err := runner.Run(patterns, config)
	if err != nil {
		log.Fatalf("error running mutations: %s", err)
	}

	// Discover tests from resolved target packages and executed test events
	var discoveredTests []string
	targets, _, err := runner.ResolveTargets(patterns)
	if err == nil {
		for _, target := range targets {
			tests, err := runner.DiscoverTests(target.Dir)
			if err == nil {
				discoveredTests = append(discoveredTests, tests...)
			}
		}
	}
	for t := range report.ExecutedTests {
		discoveredTests = append(discoveredTests, t)
	}

	stats := runner.CalculateTestStats(discoveredTests, report.TestKills)

	if jsonOut {
		data, err := runner.FormatJSONReport(report, stats, verbose)
		if err != nil {
			log.Fatalf("error formatting json report: %s", err)
		}
		fmt.Println(string(data))
	} else {
		runner.PrintHumanReport(os.Stdout, report, stats, verbose)
	}
}
