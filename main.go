package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/tools/go/ast/astutil"
)

const GOMUTATION = "GOMUTATION"

func usage() {
	fmt.Println("Usage:\nselene file.go")
}

func main() {
	log.SetOutput(io.Discard)

	if len(os.Args) < 2 {
		usage()
		os.Exit(0)
	}

	mutationDir := os.Getenv(GOMUTATION)
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

	log.Printf("mutation directory: %s", mutationDir)

	filenames := os.Args[1:]
	overlay, err := runMutations(filenames, mutationDir, os.Stdout)
	if err != nil {
		log.Fatalf("failed to run mutations: %s", err)
	}

	absPath, err := filepath.Abs(filenames[0])
	if err != nil {
		log.Fatalln(err)
	}
	dir := filepath.Dir(absPath)

	log.Printf("running go test on dir: %s", dir)

	tests, err := runGoTest(dir, overlay)
	if err != nil {
		log.Fatalf("error running go test: %s", err)
	}

	testCount := 0
	failed := 0
	for _, test := range tests {
		if test.Test == "" {
			continue
		}

		switch test.Action {
		case "run":
			fmt.Printf("=== RUN   %s\n", test.Test)
		case "pass":
			testCount++
			fmt.Printf("--- PASS: %s (%0.2fs) - MUTATION NOT CAUGHT\n", test.Test, test.Elapsed)
		case "fail":
			testCount++
			failed++
			fmt.Printf("--- FAIL: %s (%0.2fs) - MUTATION CAUGHT\n", test.Test, test.Elapsed)
		}
	}

	if failed != testCount {
		fmt.Printf("FAIL\n%d out of %d tests didn't catch any mutations\n", testCount-failed, testCount)
		os.Exit(1)
	}

	fmt.Println("PASS")
}

type TestEvent struct {
	Time    time.Time // encodes as an RFC3339-format string
	Action  string
	Package string
	Test    string
	Elapsed float64 // seconds
	Output  string
}

func parseGoTestOutput(test []byte) ([]TestEvent, error) {
	var tests []TestEvent
	list := "[" + strings.ReplaceAll(string(test[:len(test)-1]), "\n", ",") + "]"
	err := json.Unmarshal([]byte(list), &tests)
	if err != nil {
		log.Printf("raw json: %s", test)
		return nil, fmt.Errorf("error unmarshaling json: %s", err)
	}
	return tests, nil
}

func runGoTest(pkgDir, overlay string) ([]TestEvent, error) {
	out, err := exec.Command("go", "test", "--json", "--overlay", overlay, pkgDir).CombinedOutput()
	if err != nil {
		// go test returns with exit code 1 if tests fail
		// let's log just in case but move on
		log.Println(err)
	}

	return parseGoTestOutput(out)
}

func runMutations(filenames []string, mutationDir string, output io.Writer) (string, error) {
	overlays := map[string]string{}
	for _, filename := range filenames {
		log.Printf("source file: %s", filename)

		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, filename, nil, 0)
		if err != nil {
			return "", err
		}

		astutil.Apply(file, nil, reverseIfCond)
		mutatedFile := filepath.Join(mutationDir, filepath.Base(filename))

		log.Printf("mutated file: %s", mutatedFile)
		f, err := os.Create(mutatedFile)
		if err != nil {
			return "", err
		}
		defer f.Close()

		printer.Fprint(f, fset, file)
		overlays[filename] = mutatedFile
	}

	type ov struct {
		Replace map[string]string
	}

	bytes, err := json.Marshal(ov{Replace: overlays})
	if err != nil {
		return "", err
	}

	overlay := filepath.Join(mutationDir, "overlay.json")
	log.Printf("overlay file: %s", overlay)

	f, err := os.Create(overlay)
	if err != nil {
		return "", err
	}
	fmt.Fprintf(f, "%s", bytes)

	return overlay, nil
}

func reverseIfCond(c *astutil.Cursor) bool {
	n := c.Node()
	switch x := n.(type) {
	case *ast.IfStmt:
		bin, ok := x.Cond.(*ast.BinaryExpr)
		if ok {
			notBin := &ast.UnaryExpr{
				Op: token.NOT,
				X:  bin,
			}
			x.Cond = notBin
		}
	}

	return true
}
