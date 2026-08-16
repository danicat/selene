package runner

import (
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strings"
)

// ParentTestName returns the root test name by stripping subtest hierarchy.
// In Go, subtests are reported with slashes (e.g. "TestFoo/Sub" -> "TestFoo",
// "TestParent/Sub/Child" -> "TestParent"). Plain test names return unchanged.
func ParentTestName(testName string) string {
	if before, _, ok := strings.Cut(testName, "/"); ok {
		return before
	}
	return testName
}

// AggregateTestKills aggregates mutation IDs under their root parent test names.
// It deduplicates mutation IDs for each parent test and sorts them deterministically.
func AggregateTestKills(rawKills map[string][]string) map[string][]string {
	aggregated := make(map[string][]string)
	if len(rawKills) == 0 {
		return aggregated
	}

	testMutMap := make(map[string]map[string]struct{})
	for testName, mutIDs := range rawKills {
		parent := ParentTestName(testName)
		if parent == "" {
			continue
		}
		if testMutMap[parent] == nil {
			testMutMap[parent] = make(map[string]struct{})
		}
		for _, id := range mutIDs {
			if id != "" {
				testMutMap[parent][id] = struct{}{}
			}
		}
	}

	for parent, mutSet := range testMutMap {
		ids := make([]string, 0, len(mutSet))
		for id := range mutSet {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		aggregated[parent] = ids
	}

	return aggregated
}

// IsContainerTest returns true if testName acts as an umbrella harness for subtests
// within the provided collection of known tests (i.e. there exists a test starting with testName + "/").
func IsContainerTest(testName string, allTests []string) bool {
	prefix := testName + "/"
	for _, t := range allTests {
		if strings.HasPrefix(t, prefix) {
			return true
		}
	}
	return false
}

// ExtractLeafTests filters out umbrella container tests and returns only standalone
// unit tests and individual table-driven subtest cases.
func ExtractLeafTests(allTests []string) []string {
	seen := make(map[string]bool)
	for _, t := range allTests {
		t = strings.TrimSpace(t)
		if t != "" {
			seen[t] = true
		}
	}

	unique := make([]string, 0, len(seen))
	for t := range seen {
		unique = append(unique, t)
	}

	var leaves []string
	for _, t := range unique {
		if !IsContainerTest(t, unique) {
			leaves = append(leaves, t)
		}
	}
	sort.Strings(leaves)
	return leaves
}

// TestStats contains test classification metrics and quality scores.
type TestStats struct {
	TotalTests       int                 `json:"total_tests"`
	GoodTests        []string            `json:"good_tests"`
	ZeroKillTests    []string            `json:"zero_kill_tests"`
	TestQualityScore float64             `json:"test_quality_score"`
	AggregatedKills  map[string][]string `json:"aggregated_kills,omitempty"`
}

// CalculateTestStats classifies tests into Good and ZeroKill tests and computes TestQualityScore
// based strictly on leaf tests (standalone tests and individual subtest cases).
// Umbrella container tests (e.g. parent functions of table-driven tests) are excluded
// from the total test denominator to prevent double-counting.
func CalculateTestStats(discoveredTests []string, rawKills map[string][]string) TestStats {
	allCandidates := make(map[string]bool)
	for _, t := range discoveredTests {
		t = strings.TrimSpace(t)
		if t != "" {
			allCandidates[t] = true
		}
	}
	for t := range rawKills {
		t = strings.TrimSpace(t)
		if t != "" {
			allCandidates[t] = true
		}
	}

	candList := make([]string, 0, len(allCandidates))
	for t := range allCandidates {
		candList = append(candList, t)
	}

	leafTests := ExtractLeafTests(candList)

	goodSet := make(map[string]bool)
	for testName, kills := range rawKills {
		if len(kills) > 0 {
			goodSet[testName] = true
		}
	}

	var goodTests []string
	var zeroKillTests []string

	for _, t := range leafTests {
		if goodSet[t] {
			goodTests = append(goodTests, t)
			continue
		}
		parent := ParentTestName(t)
		if goodSet[parent] {
			goodTests = append(goodTests, t)
			continue
		}
		zeroKillTests = append(zeroKillTests, t)
	}
	sort.Strings(zeroKillTests)

	totalTests := len(leafTests)
	qualityScore := 0.0
	if totalTests > 0 {
		qualityScore = (float64(len(goodTests)) / float64(totalTests)) * 100.0
	}

	return TestStats{
		TotalTests:       totalTests,
		GoodTests:        goodTests,
		ZeroKillTests:    zeroKillTests,
		TestQualityScore: qualityScore,
		AggregatedKills:  rawKills,
	}
}

// ExcludedMutant represents metadata and reason for a safety-excluded mutation.
type ExcludedMutant struct {
	MutantID string `json:"mutant_id"`
	Mutator  string `json:"mutator"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Col      int    `json:"col"`
	Reason   string `json:"reason"`
}

// JSONReport represents the structured JSON output for mutation testing results.
type JSONReport struct {
	TotalMutations    int                 `json:"total_mutations"`
	Killed            int                 `json:"killed"`
	Survived          int                 `json:"survived"`
	Timeouts          int                 `json:"timeouts"`
	Uncovered         int                 `json:"uncovered"`
	Excluded          int                 `json:"excluded"`
	BuildFailures     int                 `json:"build_failures,omitempty"`
	TotalTests        int                 `json:"total_tests"`
	GoodTests         []string            `json:"good_tests"`
	ZeroKillTests     []string            `json:"zero_kill_tests"`
	MutationScore     float64             `json:"mutation_score"`
	TestQualityScore  float64             `json:"test_quality_score"`
	TestKills         map[string][]string `json:"test_kills,omitempty"`
	ExcludedMutations []ExcludedMutant    `json:"excluded_mutations,omitempty"`
}

// NewJSONReport constructs a JSONReport from Report and TestStats.
func NewJSONReport(report *Report, stats TestStats, verbose bool) JSONReport {
	var totalMut, killed, survived, timeouts, uncovered, excluded, buildFailures int
	var mutScore float64
	var excludedList []ExcludedMutant
	if report != nil {
		totalMut = report.Total
		killed = report.Killed
		survived = report.Survived
		timeouts = report.Timeouts
		uncovered = report.Uncovered
		excluded = report.Excluded
		buildFailures = report.BuildFailures
		mutScore = report.Score()
		excludedList = report.ExcludedList
	}

	goodTests := stats.GoodTests
	if goodTests == nil {
		goodTests = []string{}
	}
	zeroKillTests := stats.ZeroKillTests
	if zeroKillTests == nil {
		zeroKillTests = []string{}
	}

	jr := JSONReport{
		TotalMutations:    totalMut,
		Killed:            killed,
		Survived:          survived,
		Timeouts:          timeouts,
		Uncovered:         uncovered,
		Excluded:          excluded,
		BuildFailures:     buildFailures,
		TotalTests:        stats.TotalTests,
		GoodTests:         goodTests,
		ZeroKillTests:     zeroKillTests,
		MutationScore:     mutScore,
		TestQualityScore:  stats.TestQualityScore,
		ExcludedMutations: excludedList,
	}

	if verbose {
		if stats.AggregatedKills != nil {
			jr.TestKills = stats.AggregatedKills
		} else if report != nil {
			jr.TestKills = report.TestKills
		}
	}

	return jr
}

// FormatJSONReport serializes Report and TestStats into indented JSON.
func FormatJSONReport(report *Report, stats TestStats, verbose bool) ([]byte, error) {
	jr := NewJSONReport(report, stats, verbose)
	return json.MarshalIndent(jr, "", "  ")
}

// PrintHumanReport formats and prints human-readable mutation testing summary to w.
func PrintHumanReport(w io.Writer, report *Report, stats TestStats, verbose bool) {
	var totalMut, killed, timeouts, survived, uncovered, excluded, buildFailures int
	var mutScore float64
	if report != nil {
		totalMut = report.Total
		killed = report.Killed
		timeouts = report.Timeouts
		survived = report.Survived
		uncovered = report.Uncovered
		excluded = report.Excluded
		buildFailures = report.BuildFailures
		mutScore = report.Score()
	}

	fmt.Fprintf(w, "\nTotal mutations: %d\n", totalMut)
	fmt.Fprintf(w, "Killed:          %d\n", killed)
	fmt.Fprintf(w, "Timeouts:        %d\n", timeouts)
	fmt.Fprintf(w, "Survived:        %d\n", survived)
	fmt.Fprintf(w, "Uncovered:       %d\n", uncovered)
	if excluded > 0 {
		fmt.Fprintf(w, "Safety-excluded: %d\n", excluded)
	}
	if buildFailures > 0 {
		fmt.Fprintf(w, "Build Failures:  %d\n", buildFailures)
	}

	if report != nil && len(report.ExcludedList) > 0 {
		fmt.Fprintln(w, "\nSafety-excluded mutations:")
		for _, ex := range report.ExcludedList {
			fmt.Fprintf(w, "- %s:%d:%d: %s (%s)\n", ex.File, ex.Line, ex.Col, ex.Mutator, ex.Reason)
		}
	}

	fmt.Fprintf(w, "\nTotal tests:     %d\n", stats.TotalTests)
	fmt.Fprintf(w, "Good tests:      %d\n", len(stats.GoodTests))
	fmt.Fprintf(w, "Zero-kill tests: %d\n", len(stats.ZeroKillTests))

	if len(stats.ZeroKillTests) > 0 {
		fmt.Fprintln(w, "\nZero-kill tests (caught 0 mutations):")
		for _, test := range stats.ZeroKillTests {
			fmt.Fprintf(w, "- %s\n", test)
		}
	}

	if verbose && len(stats.GoodTests) > 0 {
		fmt.Fprintln(w, "\nGood tests details:")
		killsMap := stats.AggregatedKills
		if killsMap == nil && report != nil {
			killsMap = report.TestKills
		}
		for _, test := range stats.GoodTests {
			kills := killsMap[test]
			fmt.Fprintf(w, "- %s (caught %d mutations): %s\n", test, len(kills), strings.Join(kills, ", "))
		}
	}

	fmt.Fprintf(w, "\nMutation Score:     %.2f%% (killed/total mutations)\n", mutScore)
	fmt.Fprintf(w, "Test Quality Score: %.2f%% (good tests/total tests)\n", stats.TestQualityScore)
}

// DiscoverTests discovers all top-level test functions in the specified package directory.
func DiscoverTests(pkgDir string) ([]string, error) {
	cmd := exec.Command("go", "test", "-list", ".")
	cmd.Dir = pkgDir
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var tests []string
	lines := strings.SplitSeq(string(out), "\n")
	for line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Test") {
			tests = append(tests, line)
		}
	}
	return tests, nil
}
