package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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

		var files []string
		for _, arg := range args {
			if strings.Contains(arg, "...") {
				// Use go list to find all files in the pattern
				out, err := exec.Command("go", "list", "-f", "{{range .GoFiles}}{{.}} {{end}}", arg).Output()
				if err != nil {
					// Fallback to original arg if go list fails (e.g. not in a module)
					files = append(files, arg)
					continue
				}

				// Get the package directories to build absolute paths
				dirOut, err := exec.Command("go", "list", "-f", "{{.Dir}}", arg).Output()
				if err != nil {
					files = append(files, arg)
					continue
				}

				dirs := strings.Split(strings.TrimSpace(string(dirOut)), "\n")
				fileLists := strings.Split(strings.TrimSpace(string(out)), "\n")

				for i, fileList := range fileLists {
					if i >= len(dirs) {
						break
					}
					dir := dirs[i]
					fs := strings.Fields(fileList)
					for _, f := range fs {
						files = append(files, filepath.Join(dir, f))
					}
				}
			} else {
				files = append(files, arg)
			}
		}

		if len(files) == 0 {
			fmt.Println("No files found to mutate.")
			return
		}

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

		results, err := runner.RunIterative(files, mutationDir, mutators, coverage)
		if err != nil {
			fmt.Printf("Error running mutations: %s\n", err)
			os.Exit(1)
		}

		killed := 0
		survived := 0
		uncovered := 0
		for _, r := range results {
			switch r.Status {
			case "killed":
				killed++
			case "survived":
				survived++
			case "uncovered":
				uncovered++
			}
			fmt.Printf("%s: %s\n", r.ID, r.Status)
		}

		covered := killed + survived
		score := 0.0
		if covered > 0 {
			score = float64(killed) / float64(covered) * 100
		}

		fmt.Printf("\nTotal mutations: %d\n", len(results))
		fmt.Printf("Killed:          %d\n", killed)
		fmt.Printf("Survived:        %d\n", survived)
		fmt.Printf("Uncovered:       %d\n", uncovered)
		fmt.Printf("Mutation Score:  %.2f%% (killed/covered)\n", score)
		
		if killed < covered {
			os.Exit(1)
		}
	},
}

func init() {
	rootCmd.AddCommand(runCmd)
	runCmd.Flags().String("mutation-dir", "", "Directory to store mutations")
	runCmd.Flags().StringSlice("mutators", []string{}, "List of mutators to enable (comma-separated). If empty, all mutators are enabled.")
}
