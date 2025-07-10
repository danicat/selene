package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"golang.org/x/tools/go/ast/astutil"
)

func TestParseGoTestOutput(t *testing.T) {
	// Helper to create time.Time from RFC3339 string
	parseTime := func(s string) time.Time {
		tm, _ := time.Parse(time.RFC3339, s)
		return tm
	}

	tests := []struct {
		name    string
		input   []byte
		want    []TestEvent
		wantErr bool
	}{
		{
			name:  "single run event",
			input: []byte(`{"Time":"2023-10-27T10:00:00Z","Action":"run","Package":"example.com/mypkg","Test":"TestMyFunction"}` + "\n"),
			want: []TestEvent{
				{Time: parseTime("2023-10-27T10:00:00Z"), Action: "run", Package: "example.com/mypkg", Test: "TestMyFunction"},
			},
			wantErr: false,
		},
		{
			name:  "single pass event",
			input: []byte(`{"Time":"2023-10-27T10:00:01Z","Action":"pass","Package":"example.com/mypkg","Test":"TestMyFunction","Elapsed":0.123}` + "\n"),
			want: []TestEvent{
				{Time: parseTime("2023-10-27T10:00:01Z"), Action: "pass", Package: "example.com/mypkg", Test: "TestMyFunction", Elapsed: 0.123},
			},
			wantErr: false,
		},
		{
			name:  "single fail event",
			input: []byte(`{"Time":"2023-10-27T10:00:02Z","Action":"fail","Package":"example.com/mypkg","Test":"TestAnotherFunction","Elapsed":0.456,"Output":"some error output"}` + "\n"),
			want: []TestEvent{
				{Time: parseTime("2023-10-27T10:00:02Z"), Action: "fail", Package: "example.com/mypkg", Test: "TestAnotherFunction", Elapsed: 0.456, Output: "some error output"},
			},
			wantErr: false,
		},
		{
			name: "sequence of events",
			input: []byte(`{"Time":"2023-10-27T10:00:00Z","Action":"run","Package":"example.com/mypkg","Test":"TestMyFunction"}` + "\n" +
				`{"Time":"2023-10-27T10:00:01Z","Action":"pass","Package":"example.com/mypkg","Test":"TestMyFunction","Elapsed":0.123}` + "\n"),
			want: []TestEvent{
				{Time: parseTime("2023-10-27T10:00:00Z"), Action: "run", Package: "example.com/mypkg", Test: "TestMyFunction"},
				{Time: parseTime("2023-10-27T10:00:01Z"), Action: "pass", Package: "example.com/mypkg", Test: "TestMyFunction", Elapsed: 0.123},
			},
			wantErr: false,
		},
		{
			name:    "empty input",
			input:   []byte(""),
			want:    nil, // Expecting an error or empty slice, depends on implementation
			wantErr: true, // Assuming empty or malformed JSON is an error
		},
		{
			name:    "malformed json",
			input:   []byte(`{"Time":"2023-10-27T10:00:00Z","Action":"run",`),
			want:    nil,
			wantErr: true,
		},
		{
			name: "event with no test name (should be skipped by main logic, but parsed)",
			input: []byte(`{"Time":"2023-10-27T10:00:03Z","Action":"output","Package":"example.com/mypkg","Output":"some build output"}` + "\n"),
			want: []TestEvent{
				{Time: parseTime("2023-10-27T10:00:03Z"), Action: "output", Package: "example.com/mypkg", Output: "some build output"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseGoTestOutput(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseGoTestOutput() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				// For better error messages, especially with time.Time
				if len(got) != len(tt.want) {
					t.Errorf("parseGoTestOutput() got = %v, want %v (length mismatch)", got, tt.want)
					return
				}
				for i := range got {
					if !reflect.DeepEqual(got[i], tt.want[i]) {
						t.Errorf("parseGoTestOutput() event mismatch at index %d\ngot  = %+v\nwant = %+v", i, got[i], tt.want[i])
					}
					// Specifically check Time if other fields match, as DeepEqual might be tricky with it
					if !got[i].Time.Equal(tt.want[i].Time) && reflect.DeepEqual(rmTime(got[i]), rmTime(tt.want[i])) {
						t.Errorf("parseGoTestOutput() event mismatch at index %d on Time field\ngot.Time  = %v\nwant.Time = %v", i, got[i].Time, tt.want[i].Time)
					}
				}
				// Fallback if detailed check didn't trigger but still not equal
				if !reflect.DeepEqual(got, tt.want) {
					t.Errorf("parseGoTestOutput() got = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

// Helper function to create a TestEvent without the Time field for comparison
func rmTime(e TestEvent) TestEvent {
	e.Time = time.Time{}
	return e
}

func TestReverseIfCond(t *testing.T) {
	tests := []struct {
		name       string
		inputCode  string
		expectMutation bool // True if we expect the IfStmt condition to change
		expectedCond func(t *testing.T, cond ast.Expr) // specific check for the mutated condition
		expectedCode string // Optional: for string comparison of the whole function
	}{
		{
			name: "simple binary condition",
			inputCode: `package p
func f() {
	if a == b {
		return
	}
}`,
			expectMutation: true,
			expectedCond: func(t *testing.T, cond ast.Expr) {
				unaryExpr, ok := cond.(*ast.UnaryExpr)
				if !ok {
					t.Fatalf("Expected UnaryExpr, got %T", cond)
				}
				if unaryExpr.Op != token.NOT {
					t.Errorf("Expected token.NOT, got %s", unaryExpr.Op)
				}
				if _, ok := unaryExpr.X.(*ast.BinaryExpr); !ok {
					t.Errorf("Expected UnaryExpr.X to be BinaryExpr, got %T", unaryExpr.X)
				}
			},
			expectedCode: `package p

func f() {
	if !(a == b) {
		return
	}
}
`,
		},
		{
			name: "if with single identifier condition",
			inputCode: `package p
func f() {
	if x {
		return
	}
}`,
			expectMutation: false, // reverseIfCond only looks for BinaryExpr
			expectedCond: func(t *testing.T, cond ast.Expr) {
				ident, ok := cond.(*ast.Ident)
				if !ok {
					t.Fatalf("Expected Ident, got %T", cond)
				}
				if ident.Name != "x" {
					t.Errorf("Expected ident.Name 'x', got %s", ident.Name)
				}
			},
			expectedCode: `package p

func f() {
	if x {
		return
	}
}
`,
		},
		{
			name: "nested if statements",
			inputCode: `package p
func f() {
	if a == b {
		if c == d {
			return
		}
	}
}`,
			expectMutation: true, // Both are binary expressions, so both should be mutated
			// We will check the outer and inner if conditions by inspecting the AST
		},
		{
			name: "no if statements",
			inputCode: `package p
func f() {
	x := 1
	return x
}`,
			expectMutation: false, // No IfStmt, so no change
			expectedCode: `package p

func f() {
	x := 1
	return x
}
`,
		},
		{
			name: "complex binary condition",
			inputCode: `package p
func f() {
	if a > b && c < d {
		return
	}
}`,
			expectMutation: true,
			expectedCond: func(t *testing.T, cond ast.Expr) {
				unaryExpr, ok := cond.(*ast.UnaryExpr)
				if !ok {
					t.Fatalf("Expected UnaryExpr, got %T", cond)
				}
				if unaryExpr.Op != token.NOT {
					t.Errorf("Expected token.NOT, got %s", unaryExpr.Op)
				}
				binaryExpr, ok := unaryExpr.X.(*ast.BinaryExpr)
				if !ok {
					t.Errorf("Expected UnaryExpr.X to be BinaryExpr, got %T", unaryExpr.X)
				}
				if binaryExpr.Op != token.LAND {
					t.Errorf("Expected inner binary op to be &&, got %s", binaryExpr.Op)
				}
			},
			expectedCode: `package p

func f() {
	if !(a > b && c < d) {
		return
	}
}
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "input.go", tt.inputCode, parser.ParseComments)
			if err != nil {
				t.Fatalf("Failed to parse input code: %v", err)
			}

			originalIfStmts := collectIfStmts(file)
			astutil.Apply(file, nil, reverseIfCond)
			mutatedIfStmts := collectIfStmts(file)

			if !tt.expectMutation && len(originalIfStmts) == 0 && len(mutatedIfStmts) == 0 {
				// No if statements and none expected, so just check code if provided
				if tt.expectedCode != "" {
					var buf bytes.Buffer
					if err := printer.Fprint(&buf, fset, file); err != nil {
						t.Fatalf("Failed to print AST: %v", err)
					}
					if gotCode := strings.TrimSpace(buf.String()); strings.TrimSpace(tt.expectedCode) != gotCode {
						t.Errorf("Code mismatch:\nExpected:\n%s\nGot:\n%s", tt.expectedCode, gotCode)
					}
				}
				return // Test passes
			}

			if len(originalIfStmts) == 0 && tt.expectMutation {
				t.Fatalf("Expected mutation, but no if statements found in input code.")
			}
			if len(originalIfStmts) > 0 && !tt.expectMutation {
                 // Check if any condition was unintentionally mutated
                for i := range originalIfStmts {
                    if !reflect.DeepEqual(originalIfStmts[i].Cond, mutatedIfStmts[i].Cond) {
                        t.Errorf("Expected no mutation, but condition changed for if statement %d", i)
						var origBuf, mutBuf bytes.Buffer
						printer.Fprint(&origBuf, fset, originalIfStmts[i].Cond)
						printer.Fprint(&mutBuf, fset, mutatedIfStmts[i].Cond)
						t.Errorf("Original cond: %s, Mutated cond: %s", origBuf.String(), mutBuf.String())
                    }
                }
            }


			if tt.name == "nested if statements" {
				// Specific checks for nested ifs
				if len(mutatedIfStmts) != 2 {
					t.Fatalf("Expected 2 if statements, got %d", len(mutatedIfStmts))
				}
				// Check outer if
				outerIf, ok := mutatedIfStmts[0].Cond.(*ast.UnaryExpr)
				if !ok || outerIf.Op != token.NOT {
					t.Errorf("Outer if was not negated as expected. Got: %T", mutatedIfStmts[0].Cond)
				}
				// Check inner if (which is inside the Body of the first IfStmt)
				innerIfNode := mutatedIfStmts[0].Body.List[0] // First statement in the outer if's body
				innerIfStmt, ok := innerIfNode.(*ast.IfStmt)
				if !ok {
					t.Fatalf("Expected inner statement to be IfStmt, got %T", innerIfNode)
				}
				innerIfCond, ok := innerIfStmt.Cond.(*ast.UnaryExpr)
				if !ok || innerIfCond.Op != token.NOT {
					t.Errorf("Inner if was not negated as expected. Got: %T", innerIfStmt.Cond)
				}
			} else if len(mutatedIfStmts) > 0 && tt.expectedCond != nil {
				// For single if statement cases that expect mutation
				if len(mutatedIfStmts) != 1 {
                     // This might happen if an 'if' was expected but not found, or too many were found.
                     // Let's check if originalIfStmts had any to begin with.
                    if len(originalIfStmts) == 0 && tt.expectMutation {
                        t.Fatalf("Expected mutation and one if statement, but found none in original code.")
                    } else if len(originalIfStmts) > 0 && tt.expectMutation && len(mutatedIfStmts) != len(originalIfStmts) {
                        // This case implies reverseIfCond might have added/removed IfStmts, which it shouldn't.
                        t.Fatalf("Number of if statements changed during mutation. Original: %d, Mutated: %d", len(originalIfStmts), len(mutatedIfStmts))
                    } else if len(mutatedIfStmts) != 1 {
                        // General case if we expected one mutated IfStmt but got a different number.
					    t.Fatalf("Expected 1 if statement, got %d", len(mutatedIfStmts))
                    }
				}
                // If we are here, len(mutatedIfStmts) is likely 1, or the test logic needs refinement for multiple non-nested ifs.
                // Assuming the first IfStmt is the one of interest for non-nested cases.
				tt.expectedCond(t, mutatedIfStmts[0].Cond)
			}


			// Fallback to string comparison if expectedCode is provided
			if tt.expectedCode != "" {
				var buf bytes.Buffer
				// Create a new fset for printing to ensure correct formatting from the modified AST
				printFset := token.NewFileSet()
				err := printer.Fprint(&buf, printFset, file) // Use 'file' which is the root of the AST
				if err != nil {
					t.Fatalf("Failed to print AST to string: %v", err)
				}
				gotCode := strings.TrimSpace(buf.String())
				expected := strings.TrimSpace(tt.expectedCode)
				if expected != gotCode {
					t.Errorf("Code string mismatch:\nExpected:\n%s\nGot:\n%s", expected, gotCode)
				}
			}
		})
	}
}

// collectIfStmts is a helper to gather all IfStmts from an AST
func collectIfStmts(node ast.Node) []*ast.IfStmt {
	var stmts []*ast.IfStmt
	ast.Inspect(node, func(n ast.Node) bool {
		if ifStmt, ok := n.(*ast.IfStmt); ok {
			stmts = append(stmts, ifStmt)
		}
		return true
	})
	return stmts
}

// TestRunMutations_Integration performs an integration test on the runMutations function.
func TestRunMutations_Integration(t *testing.T) {
	// 1. Create a temporary directory structure for test artifacts
	sourceDir := t.TempDir() // Base for source files, cleaned up automatically
	mutationOutDir := t.TempDir() // Output for mutations, cleaned up automatically

	sourceFilePath := filepath.Join(sourceDir, "calc.go")
	sourceFileContent := `package source
func Abs(n int) int {
	if n < 0 { // This condition will be mutated
		return -n
	}
	return n
}

func Add(a, b int) int {
	if a == 0 { // This should also be mutated
		return b
	}
	return a + b
}
`
	err := os.WriteFile(sourceFilePath, []byte(sourceFileContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write source file: %v", err)
	}

	// 2. Call runMutations
	// For the output writer, we can use io.Discard if we don't need to check stdout
	overlayFilePath, err := runMutations([]string{sourceFilePath}, mutationOutDir, io.Discard)
	if err != nil {
		t.Fatalf("runMutations returned an error: %v", err)
	}

	// 3. Verifications
	// 3a. An overlay JSON file is created
	expectedOverlayPath := filepath.Join(mutationOutDir, "overlay.json")
	if _, err := os.Stat(expectedOverlayPath); os.IsNotExist(err) {
		t.Fatalf("Overlay JSON file was not created at %s", expectedOverlayPath)
	}
	if overlayFilePath != expectedOverlayPath {
		t.Errorf("runMutations returned overlay path %s, but expected %s", overlayFilePath, expectedOverlayPath)
	}

	// 3b. Read and parse the overlay JSON
	overlayBytes, err := os.ReadFile(expectedOverlayPath)
	if err != nil {
		t.Fatalf("Failed to read overlay.json: %v", err)
	}

	type OverlayJSON struct {
		Replace map[string]string
	}
	var parsedOverlay OverlayJSON
	err = json.Unmarshal(overlayBytes, &parsedOverlay)
	if err != nil {
		t.Fatalf("Failed to unmarshal overlay JSON: %v. Content: %s", err, string(overlayBytes))
	}

	// 3c. The "Replace" map should have an entry for the source file
	if len(parsedOverlay.Replace) != 1 {
		t.Fatalf("Expected 1 entry in Replace map, got %d. Content: %+v", len(parsedOverlay.Replace), parsedOverlay.Replace)
	}

	absSourceFilePath, err := filepath.Abs(sourceFilePath)
	if err != nil {
		t.Fatalf("Failed to get absolute path for source file: %v", err)
	}

	mutatedFilePathInOverlay, ok := parsedOverlay.Replace[absSourceFilePath]
	if !ok {
		// Try also with the direct sourceFilePath if absolute path didn't match
		// This can happen if runMutations stores it non-abs internally before json marshalling.
		// Based on runMutations implementation, it seems to use the filename as passed.
		mutatedFilePathInOverlay, ok = parsedOverlay.Replace[sourceFilePath]
		if !ok {
			t.Fatalf("Overlay JSON does not contain replacement for source file %s (or %s). Got: %+v", sourceFilePath, absSourceFilePath, parsedOverlay.Replace)
		}
	}

	// Check if the path stored in overlay is absolute. If not, join it with mutationOutDir.
	// The current implementation of runMutations stores the filename as key and an absolute path to mutated file as value.
	// So, mutatedFilePathInOverlay should be an absolute path.
	expectedMutatedFilePath := filepath.Join(mutationOutDir, filepath.Base(sourceFilePath))
	if mutatedFilePathInOverlay != expectedMutatedFilePath {
		// It's possible runMutations now makes it absolute. Let's check that too.
		absExpectedMutatedFilePath, _ := filepath.Abs(expectedMutatedFilePath)
		if mutatedFilePathInOverlay != absExpectedMutatedFilePath {
			t.Errorf("Mutated file path in overlay JSON mismatch.\nExpected: %s or %s\nGot: %s",
				expectedMutatedFilePath, absExpectedMutatedFilePath, mutatedFilePathInOverlay)
		}
	}


	// 3d. The mutated calc.go file exists
	if _, err := os.Stat(mutatedFilePathInOverlay); os.IsNotExist(err) {
		t.Fatalf("Mutated source file does not exist at path from overlay: %s", mutatedFilePathInOverlay)
	}

	// 3e. Read and verify the content of the mutated calc.go
	mutatedFileBytes, err := os.ReadFile(mutatedFilePathInOverlay)
	if err != nil {
		t.Fatalf("Failed to read mutated source file %s: %v", mutatedFilePathInOverlay, err)
	}

	fset := token.NewFileSet()
	mutatedAST, err := parser.ParseFile(fset, filepath.Base(mutatedFilePathInOverlay), string(mutatedFileBytes), 0)
	if err != nil {
		t.Fatalf("Failed to parse mutated source file content: %v", err)
	}

	ifStmts := collectIfStmts(mutatedAST)
	if len(ifStmts) != 2 { // We have two functions with 'if'
		t.Fatalf("Expected 2 if statements in mutated AST, got %d", len(ifStmts))
	}

	for i, ifStmt := range ifStmts {
		cond := ifStmt.Cond
		unaryExpr, ok := cond.(*ast.UnaryExpr)
		if !ok {
			t.Errorf("Expected if statement %d condition to be UnaryExpr, got %T", i, cond)
			continue
		}
		if unaryExpr.Op != token.NOT {
			t.Errorf("Expected if statement %d UnaryExpr.Op to be token.NOT, got %s", i, unaryExpr.Op)
		}
		if _, ok := unaryExpr.X.(*ast.BinaryExpr); !ok {
			t.Errorf("Expected if statement %d UnaryExpr.X to be BinaryExpr, got %T", i, unaryExpr.X)
		}

		// Optionally, print the mutated condition for inspection
		var condBuf bytes.Buffer
		printer.Fprint(&condBuf, fset, unaryExpr)
		t.Logf("Mutated condition for if stmt %d: %s", i, condBuf.String())
	}
}

// TestRunGoTest_Integration performs an integration test on the runGoTest function.
func TestRunGoTest_Integration(t *testing.T) {
	pkgDir := t.TempDir()
	mutatedSrcDir := t.TempDir()
	overlayDir := t.TempDir()

	// 1. Create sample Go package
	libGoContent := `package samplepkg
func CheckValue(x int) bool {
	if x > 10 { // Mutated to !(x > 10)
		return true
	}
	return false
}`
	libGoPath := filepath.Join(pkgDir, "lib.go")
	if err := os.WriteFile(libGoPath, []byte(libGoContent), 0644); err != nil {
		t.Fatalf("Failed to write lib.go: %v", err)
	}

	libTestGoContent := `package samplepkg
import "testing"
func TestCheckValue_DetectsMutation(t *testing.T) {
	// With original code: CheckValue(20) is true. Test passes.
	// With mutated code: CheckValue(20) is !(20 > 10) -> !true -> false. Test fails.
	if !CheckValue(20) {
		t.Errorf("CheckValue(20) was false, expected true. Mutation likely occurred and was caught.")
	}
}
func TestCheckValue_Unaffected(t *testing.T) {
	// Original: CheckValue(5) is false. Test passes.
	// Mutated: CheckValue(5) is !(5 > 10) -> !false -> true. Test fails.
	// This test should also fail if mutation occurs and is effective.
	// Let's adjust to make one clearly pass and one clearly fail.
	if CheckValue(5) { // This should be false. If true, it's an error (or unexpected mutation effect)
		 t.Errorf("CheckValue(5) was true, expected false.")
	}
}

func TestCheckValue_PassesWithOriginal(t *testing.T) {
	// This test is designed to pass with the original code.
	// Original: CheckValue(20) -> true. Test passes.
	// Mutated:  CheckValue(20) -> false. This test would then fail.
	if !CheckValue(20) {
		t.Error("This test should pass with original code: CheckValue(20) is true")
	}
	// Original: CheckValue(5) -> false. Test passes.
	// Mutated:  CheckValue(5) -> true. This test would then fail.
	if CheckValue(5) {
		t.Error("This test should pass with original code: CheckValue(5) is false")
	}
}
`
	// Self-correction: The prompt asks for one test to fail (DetectsMutation) and one to pass (Unaffected).
	// Let's simplify lib_test.go for clarity of this goal.
	// TestCheckValue_DetectsMutation: Fails due to mutation.
	// TestCheckValue_Unaffected: Passes regardless of this specific mutation. (e.g. tests different func or aspect)
	// For this, Unaffected MUST NOT use CheckValue or be designed to pass with mutated CheckValue.
	// Let's make Unaffected test something trivial that always passes.

	libTestGoContent = `package samplepkg
import "testing"

// TestCheckValue_DetectsMutation is designed to FAIL when the mutation is active.
// Original: CheckValue(20) is true. !CheckValue(20) is false. Test passes.
// Mutated:  CheckValue(20) is !(20 > 10) -> false. !CheckValue(20) is true. Test logs Errorf.
func TestCheckValue_DetectsMutation(t *testing.T) {
	if !CheckValue(20) {
		t.Errorf("CheckValue(20) was false (mutation caught).")
	}
}

// TestCheckValue_PassesNormally is designed to PASS with original code.
// And also PASS with the mutated code because it tests a path not affected by the specific mutation,
// or the mutation makes it still pass.
// Original: CheckValue(5) is false. CheckValue(5) is false. Test passes.
// Mutated:  CheckValue(5) is !(5 > 10) -> !false -> true. CheckValue(5) is true. Test fails.
// This test will ALSO fail if mutation is active. This is not what's desired for "Unaffected".

// Let's redefine "Unaffected" to truly be unaffected or to pass under mutation.
// For CheckValue(5): Original is 'false'. Mutated is 'true'.
// A test `if !CheckValue(5)` would pass with original, fail with mutation.
// A test `if CheckValue(5)` would fail with original, pass with mutation.

// Redesigned tests for clarity:
// Test 1: Fails on mutation, passes on original.
// Test 2: Passes on original, and also passes on mutation (or tests something else entirely).

func TestCheckValue_SensitiveToMutation(t *testing.T) {
	// Original: CheckValue(20) is true. This test passes.
	// Mutated: CheckValue(20) is false. This test FAILS. (Desired for "DetectsMutation")
	if !CheckValue(20) {
		t.Error("CheckValue(20) expected true, got false. MUTATION CAUGHT.")
	}
}

func TestCheckValue_PassesRegardless(t *testing.T) {
	// This test needs to pass with original AND mutated code.
	// Original: CheckValue(5) is false. !CheckValue(5) is true. Test passes.
	// Mutated: CheckValue(5) is !(5>10) -> true. !CheckValue(5) is false. Test FAILS.
	// This setup is tricky with a single mutation point.

	// Let's use a different value for PassesRegardless for simpler logic.
	// CheckValue(0)
	// Original: 0 > 10 is false. Returns false. !CheckValue(0) is true. Test passes.
	// Mutated: !(0 > 10) is true. Returns true. !CheckValue(0) is false. Test FAILS.

	// The "Unaffected" or "PassesRegardless" test is hard to achieve with this specific mutation.
	// Instead, let's have one test that fails (catches), and one that passes (original behavior for non-mutated part).
	// For simplicity, the second test will just be a standard test that should pass.
	// The key is that runGoTest should report one fail and one pass.
	if CheckValue(5) { // Original: false. Mutated: true. This will also fail if mutation is active.
		t.Error("CheckValue(5) expected false, got true.")
	}
}
`

	// Final simplified lib_test.go for clear pass/fail demonstration:
	libTestGoContent = `package samplepkg
import "testing"

// TestCheckValue_SensitiveToMutation:
// Original: CheckValue(20) -> true. Test passes.
// Mutated:  CheckValue(20) -> false. Test fails, Errorf is logged.
func TestCheckValue_SensitiveToMutation(t *testing.T) {
	if !CheckValue(20) {
		t.Errorf("CheckValue(20) was false. Mutation likely caught.")
	}
}

// TestCheckValue_AlsoSensitive:
// Original: CheckValue(5) -> false. Test passes.
// Mutated:  CheckValue(5) -> true. Test fails, Errorf is logged.
func TestCheckValue_AlsoSensitive(t *testing.T) {
	if CheckValue(5) {
		t.Errorf("CheckValue(5) was true. Mutation likely caught.")
	}
}
`
	// The goal is to have one test fail due to mutation, and another pass.
	// If both are sensitive, they will both fail. We need one that is NOT sensitive.
	// Let's make the second test truly trivial and unrelated.
	libTestGoContent = `package samplepkg
import "testing"

func TestCheckValue_SensitiveToMutation(t *testing.T) {
	// Original: CheckValue(20) is true. Test assertion `if !CheckValue(20)` is `if !true` -> `if false`. No Errorf. PASSES.
	// Mutated:  CheckValue(20) is `!(20 > 10)` -> `!true` -> `false`. Test assertion `if !CheckValue(20)` is `if !false` -> `if true`. Errorf. FAILS.
	if !CheckValue(20) {
		t.Errorf("CheckValue(20) was false. Mutation likely caught.")
	}
}

func TestAlwaysPasses(t *testing.T) {
	// This test is designed to always pass and is not affected by the mutation in CheckValue.
	if false { // This will never be true
		t.Errorf("This should not happen.")
	}
}
`
	libTestGoPath := filepath.Join(pkgDir, "lib_test.go")
	if err := os.WriteFile(libTestGoPath, []byte(libTestGoContent), 0644); err != nil {
		t.Fatalf("Failed to write lib_test.go: %v", err)
	}

	// 2. Create mutated version of lib.go
	fset := token.NewFileSet()
	originalAST, err := parser.ParseFile(fset, libGoPath, nil, parser.ParseComments) // Provide content via nil to read from disk
	if err != nil {
		t.Fatalf("Failed to parse original lib.go: %v", err)
	}
	astutil.Apply(originalAST, nil, reverseIfCond) // Mutate the AST

	mutatedLibGoPath := filepath.Join(mutatedSrcDir, "lib.go")
	mutatedFile, err := os.Create(mutatedLibGoPath)
	if err != nil {
		t.Fatalf("Failed to create mutated lib.go file: %v", err)
	}
	defer mutatedFile.Close()
	if err := printer.Fprint(mutatedFile, fset, originalAST); err != nil {
		t.Fatalf("Failed to print mutated AST to file: %v", err)
	}

	// 3. Manually create an overlay JSON file
	absLibGoPath, _ := filepath.Abs(libGoPath)
	absMutatedLibGoPath, _ := filepath.Abs(mutatedLibGoPath)

	overlayData := map[string]map[string]string{
		"Replace": {
			absLibGoPath: absMutatedLibGoPath,
		},
	}
	overlayBytes, _ := json.Marshal(overlayData)
	overlayJSONPath := filepath.Join(overlayDir, "overlay.json")
	if err := os.WriteFile(overlayJSONPath, overlayBytes, 0644); err != nil {
		t.Fatalf("Failed to write overlay.json: %v", err)
	}

	// 4. Call runGoTest
	// Note: runGoTest expects pkgDir to be the directory containing the go files, not the files themselves.
	events, err := runGoTest(pkgDir, overlayJSONPath)
	if err != nil {
		// runGoTest itself might return an error if `go test` exits non-zero.
		// We should check the events array even if err is not nil, as failed tests are expected.
		t.Logf("runGoTest returned error (as expected if tests fail): %v", err)
	}

	// 5. Inspect the returned []TestEvent slice
	if len(events) == 0 && err != nil {
		// If go test command itself failed badly (e.g. not found, bad overlay path), events might be empty.
		t.Fatalf("runGoTest produced no events and an error: %v. This might indicate a problem with 'go test' execution.", err)
	}
	if len(events) == 0 {
		t.Fatalf("runGoTest produced no events. Expected test events.")
	}


	var sensitiveTestFound, alwaysPassesTestFound bool
	var sensitiveTestFailed, alwaysPassesTestPassed bool

	for _, event := range events {
		t.Logf("Test Event: %+v", event) // Log all events for debugging
		if event.Test == "TestCheckValue_SensitiveToMutation" && event.Action == "fail" {
			sensitiveTestFound = true
			sensitiveTestFailed = true
		}
		// It's possible for a test to have a "run" event then a "fail" or "pass" event.
		// We are interested in the final state (pass/fail).
		if event.Test == "TestAlwaysPasses" && event.Action == "pass" {
			alwaysPassesTestFound = true
			alwaysPassesTestPassed = true
		}
	}

	if !sensitiveTestFound {
		t.Errorf("TestCheckValue_SensitiveToMutation was not found in test events.")
	} else if !sensitiveTestFailed {
		t.Errorf("TestCheckValue_SensitiveToMutation did not fail as expected. It should fail due to mutation.")
	}

	if !alwaysPassesTestFound {
		t.Errorf("TestAlwaysPasses was not found in test events.")
	} else if !alwaysPassesTestPassed {
		t.Errorf("TestAlwaysPasses did not pass as expected.")
	}

	// More robust check: ensure there's a fail for Sensitive and pass for AlwaysPasses
	// and that we saw 'run' events too.
	actions := make(map[string]string) // testName -> final action
	for _, e := range events {
		if e.Test != "" && (e.Action == "pass" || e.Action == "fail") {
			actions[e.Test] = e.Action
		}
	}

	if actions["TestCheckValue_SensitiveToMutation"] != "fail" {
		t.Errorf("Expected TestCheckValue_SensitiveToMutation to fail, got: %s", actions["TestCheckValue_SensitiveToMutation"])
	}
	if actions["TestAlwaysPasses"] != "pass" {
		t.Errorf("Expected TestAlwaysPasses to pass, got: %s", actions["TestAlwaysPasses"])
	}

	fmt.Printf("Final Test Actions: %+v\n", actions) // For visibility in test logs if run directly
}

// The TestEvent struct from main.go might be needed here if it's not exported
// or if this test file is in a different package.
// For now, assuming it's accessible.
// type TestEvent struct {
// 	Time    time.Time
// 	Action  string
// 	Package string
// 	Test    string
// 	Elapsed float64 // seconds
// 	Output  string
// }
