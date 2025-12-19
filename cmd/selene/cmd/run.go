package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/danicat/selene/internal/mutator"
	"github.com/danicat/selene/internal/runner"
	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run [files]",
	Short: "Run mutation testing on the specified files",
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		mutationDir, _ := cmd.Flags().GetString("mutation-dir")
		if mutationDir == "" {
			tmpDir, err := os.MkdirTemp("", "mutation")
			if err != nil {
				fmt.Printf("Error creating temp dir: %s\n", err)
				os.Exit(1)
			}
			mutationDir = tmpDir
		}
		
		fmt.Printf("Mutation directory: %s\n", mutationDir)

		mutators := []mutator.Mutator{}
		mutatorNames, _ := cmd.Flags().GetStringSlice("mutators")
		
		if len(mutatorNames) == 0 {
			mutators = mutator.All()
		} else {
			for _, name := range mutatorNames {
				m, ok := mutator.Get(name)
				if !ok {
					fmt.Printf("Unknown mutator: %s\n", name)
					os.Exit(1)
				}
				mutators = append(mutators, m)
			}
		}
		
		fmt.Println("Running tests to generate coverage profile...")
		// We assume we are in the module root or a place where go test works
		cmdExec := exec.Command("go", "test", "-coverprofile=coverage.out", "./...")
		if output, err := cmdExec.CombinedOutput(); err != nil {
			fmt.Printf("Error running coverage: %s\n%s\n", err, output)
			os.Exit(1)
		}
		defer os.Remove("coverage.out")
		
		coverage, err := runner.LoadCoverage("coverage.out")
		if err != nil {
			fmt.Printf("Error loading coverage: %s\n", err)
			os.Exit(1)
		}

		results, err := runner.RunIterative(args, mutationDir, mutators, coverage)
		if err != nil {
			fmt.Printf("Error running mutations: %s\n", err)
			os.Exit(1)
		}

		killed := 0
		for _, r := range results {
			if r.Status == "killed" {
				killed++
			}
			fmt.Printf("%s: %s\n", r.ID, r.Status)
		}

		fmt.Printf("Score: %d/%d (%.2f%%)\n", killed, len(results), float64(killed)/float64(len(results))*100)
		
		if killed < len(results) {
			os.Exit(1)
		}
	},
}

func init() {
	rootCmd.AddCommand(runCmd)
	runCmd.Flags().String("mutation-dir", "", "Directory to store mutations")
	runCmd.Flags().StringSlice("mutators", []string{}, "List of mutators to enable (comma-separated). If empty, all mutators are enabled.")
}
