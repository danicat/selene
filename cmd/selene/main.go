package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/danicat/selene/internal/mutator"
	"github.com/danicat/selene/internal/runner"
)

const GOMUTATION = "GOMUTATION"

func usage() {
	fmt.Println("Usage:\nselene file.go [file2.go ...]")
}

func main() {
	var verbose bool
	var mutationDir string
	flag.BoolVar(&verbose, "v", false, "Enable verbose output")
	flag.StringVar(&mutationDir, "output", "", "Directory to store mutated files (default: temporary dir)")
	flag.Parse()

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
	}
	err := os.MkdirAll(mutationDir, os.ModePerm)
	if err != nil {
		log.Fatalf("failed to create mutation directory: %s", err)
	}
	if verbose {
		log.Printf("mutation directory: %s", mutationDir)
	}

	filenames := flag.Args()
	mutators := []mutator.Mutator{
		&mutator.ReverseIfCond{},
		&mutator.ArithmeticMutator{},
		&mutator.ComparisonMutator{},
		&mutator.BooleanMutator{},
	}

	config := runner.Config{
		Verbose:     verbose,
		MutationDir: mutationDir,
		Mutators:    mutators,
	}

	report, err := runner.Run(filenames, config)
	if err != nil {
		log.Fatalf("error running mutations: %s", err)
	}

	if report.Total == 0 {
		fmt.Println("No mutations found.")
		os.Exit(0)
	}

	fmt.Println("\n=== Mutation Testing Report ===")
	fmt.Printf("Total Mutations: %d\n", report.Total)
	fmt.Printf("Caught:          %d\n", report.Caught)
	fmt.Printf("Uncaught:        %d\n", report.Uncaught)
	fmt.Printf("Build Failures:  %d\n", report.BuildFailures)
	fmt.Printf("Mutation Score:  %.2f%%\n", report.Score())

	if report.Uncaught > 0 {
		os.Exit(1)
	}
}
