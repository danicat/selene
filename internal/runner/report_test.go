package runner

import (
	"bytes"
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestParentTestName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "plain test name",
			input:    "TestFoo",
			expected: "TestFoo",
		},
		{
			name:     "single-level subtest",
			input:    "TestComparisonMutator/EQL_to_NEQ",
			expected: "TestComparisonMutator",
		},
		{
			name:     "multi-level subtest",
			input:    "TestParent/Sub/Child/Deep",
			expected: "TestParent",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "trailing slash",
			input:    "TestTrailing/",
			expected: "TestTrailing",
		},
		{
			name:     "leading slash",
			input:    "/LeadingSlash",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := ParentTestName(tt.input)
			if actual != tt.expected {
				t.Errorf("ParentTestName(%q) = %q, want %q", tt.input, actual, tt.expected)
			}
		})
	}
}

func TestAggregateTestKills(t *testing.T) {
	t.Run("nil map", func(t *testing.T) {
		res := AggregateTestKills(nil)
		if len(res) != 0 {
			t.Errorf("expected empty map, got %v", res)
		}
	})

	t.Run("empty map", func(t *testing.T) {
		res := AggregateTestKills(map[string][]string{})
		if len(res) != 0 {
			t.Errorf("expected empty map, got %v", res)
		}
	})

	t.Run("flat tests without subtests", func(t *testing.T) {
		raw := map[string][]string{
			"TestA": {"mut2", "mut1"},
			"TestB": {"mut3"},
		}
		expected := map[string][]string{
			"TestA": {"mut1", "mut2"},
			"TestB": {"mut3"},
		}
		res := AggregateTestKills(raw)
		if !reflect.DeepEqual(res, expected) {
			t.Errorf("got %+v, want %+v", res, expected)
		}
	})

	t.Run("subtests aggregation and deduplication", func(t *testing.T) {
		raw := map[string][]string{
			"TestParent/Sub1":       {"mut1", "mut2"},
			"TestParent/Sub2":       {"mut2", "mut3"},
			"TestParent":            {"mut1"},
			"TestOther/Nested/Deep": {"mut4", "mut4"},
		}
		expected := map[string][]string{
			"TestParent": {"mut1", "mut2", "mut3"},
			"TestOther":  {"mut4"},
		}
		res := AggregateTestKills(raw)
		if !reflect.DeepEqual(res, expected) {
			t.Errorf("got %+v, want %+v", res, expected)
		}
	})

	t.Run("sorting verification", func(t *testing.T) {
		raw := map[string][]string{
			"TestSort/Sub": {"mutZ", "mutA", "mutM", "mutB"},
		}
		res := AggregateTestKills(raw)
		expected := []string{"mutA", "mutB", "mutM", "mutZ"}
		if !reflect.DeepEqual(res["TestSort"], expected) {
			t.Errorf("got %+v, want %+v", res["TestSort"], expected)
		}
	})
}

func TestExtractLeafTests(t *testing.T) {
	t.Run("standalone tests only", func(t *testing.T) {
		input := []string{"TestA", "TestB", "TestC"}
		want := []string{"TestA", "TestB", "TestC"}
		got := ExtractLeafTests(input)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("ExtractLeafTests(%v) = %v, want %v", input, got, want)
		}
	})

	t.Run("table test with container and subtests", func(t *testing.T) {
		input := []string{
			"TestArithmeticMutator",
			"TestArithmeticMutator/Add_to_Sub",
			"TestArithmeticMutator/Sub_to_Add",
			"TestAdd",
		}
		want := []string{
			"TestAdd",
			"TestArithmeticMutator/Add_to_Sub",
			"TestArithmeticMutator/Sub_to_Add",
		}
		got := ExtractLeafTests(input)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("ExtractLeafTests(%v) = %v, want %v", input, got, want)
		}
	})

	t.Run("nested subtests", func(t *testing.T) {
		input := []string{
			"TestParent",
			"TestParent/Sub",
			"TestParent/Sub/Child1",
			"TestParent/Sub/Child2",
			"TestStandalone",
		}
		want := []string{
			"TestParent/Sub/Child1",
			"TestParent/Sub/Child2",
			"TestStandalone",
		}
		got := ExtractLeafTests(input)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("ExtractLeafTests(%v) = %v, want %v", input, got, want)
		}
	})
}

func TestCalculateTestStats(t *testing.T) {
	t.Run("all good tests (100% score)", func(t *testing.T) {
		discoveredTests := []string{"TestA", "TestB"}
		rawKills := map[string][]string{
			"TestA": {"mut1"},
			"TestB": {"mut2"},
		}
		stats := CalculateTestStats(discoveredTests, rawKills)

		if stats.TotalTests != 2 {
			t.Errorf("expected TotalTests 2, got %d", stats.TotalTests)
		}
		if !reflect.DeepEqual(stats.GoodTests, []string{"TestA", "TestB"}) {
			t.Errorf("expected GoodTests [TestA, TestB], got %v", stats.GoodTests)
		}
		if len(stats.ZeroKillTests) != 0 {
			t.Errorf("expected ZeroKillTests to be empty, got %v", stats.ZeroKillTests)
		}
		if stats.TestQualityScore != 100.0 {
			t.Errorf("expected TestQualityScore 100.0, got %f", stats.TestQualityScore)
		}
	})

	t.Run("all zero kill tests (0% score)", func(t *testing.T) {
		discoveredTests := []string{"TestA", "TestB"}
		rawKills := map[string][]string{}
		stats := CalculateTestStats(discoveredTests, rawKills)

		if stats.TotalTests != 2 {
			t.Errorf("expected TotalTests 2, got %d", stats.TotalTests)
		}
		if len(stats.GoodTests) != 0 {
			t.Errorf("expected GoodTests to be empty, got %v", stats.GoodTests)
		}
		if !reflect.DeepEqual(stats.ZeroKillTests, []string{"TestA", "TestB"}) {
			t.Errorf("expected ZeroKillTests [TestA, TestB], got %v", stats.ZeroKillTests)
		}
		if stats.TestQualityScore != 0.0 {
			t.Errorf("expected TestQualityScore 0.0, got %f", stats.TestQualityScore)
		}
	})

	t.Run("mixed good and zero kill tests", func(t *testing.T) {
		discoveredTests := []string{"TestA", "TestB", "TestC", "TestD"}
		rawKills := map[string][]string{
			"TestA": {"mut1"},
			"TestC": {"mut2"},
		}
		stats := CalculateTestStats(discoveredTests, rawKills)

		if stats.TotalTests != 4 {
			t.Errorf("expected TotalTests 4, got %d", stats.TotalTests)
		}
		if !reflect.DeepEqual(stats.GoodTests, []string{"TestA", "TestC"}) {
			t.Errorf("expected GoodTests [TestA, TestC], got %v", stats.GoodTests)
		}
		if !reflect.DeepEqual(stats.ZeroKillTests, []string{"TestB", "TestD"}) {
			t.Errorf("expected ZeroKillTests [TestB, TestD], got %v", stats.ZeroKillTests)
		}
		if stats.TestQualityScore != 50.0 {
			t.Errorf("expected TestQualityScore 50.0, got %f", stats.TestQualityScore)
		}
	})

	t.Run("subtest leaf scoring without container double-counting", func(t *testing.T) {
		discoveredTests := []string{
			"TestMutator",
			"TestMutator/Sub1",
			"TestMutator/Sub2",
			"TestStandaloneZeroKill",
		}
		rawKills := map[string][]string{
			"TestMutator/Sub1": {"mut1"},
		}
		stats := CalculateTestStats(discoveredTests, rawKills)

		// Leaf tests: TestMutator/Sub1, TestMutator/Sub2, TestStandaloneZeroKill (3 total, TestMutator excluded)
		if stats.TotalTests != 3 {
			t.Errorf("expected TotalTests 3, got %d", stats.TotalTests)
		}
		if !reflect.DeepEqual(stats.GoodTests, []string{"TestMutator/Sub1"}) {
			t.Errorf("expected GoodTests [TestMutator/Sub1], got %v", stats.GoodTests)
		}
		expectedZeroKill := []string{"TestMutator/Sub2", "TestStandaloneZeroKill"}
		if !reflect.DeepEqual(stats.ZeroKillTests, expectedZeroKill) {
			t.Errorf("expected ZeroKillTests %v, got %v", expectedZeroKill, stats.ZeroKillTests)
		}
		expectedScore := (1.0 / 3.0) * 100.0
		if math.Abs(stats.TestQualityScore-expectedScore) > 0.001 {
			t.Errorf("expected TestQualityScore %f, got %f", expectedScore, stats.TestQualityScore)
		}
	})

	t.Run("empty inputs", func(t *testing.T) {
		stats := CalculateTestStats(nil, nil)
		if stats.TotalTests != 0 {
			t.Errorf("expected TotalTests 0, got %d", stats.TotalTests)
		}
		if len(stats.GoodTests) != 0 {
			t.Errorf("expected GoodTests empty, got %v", stats.GoodTests)
		}
		if len(stats.ZeroKillTests) != 0 {
			t.Errorf("expected ZeroKillTests empty, got %v", stats.ZeroKillTests)
		}
		if stats.TestQualityScore != 0.0 {
			t.Errorf("expected TestQualityScore 0.0, got %f", stats.TestQualityScore)
		}
	})
}

func TestJSONReport(t *testing.T) {
	report := &Report{
		Total:         10,
		Killed:        6,
		Timeouts:      2,
		Survived:      1,
		Uncovered:     1,
		BuildFailures: 1,
		TestKills: map[string][]string{
			"TestA/Sub": {"mut1", "mut2"},
		},
	}
	stats := TestStats{
		TotalTests:       2,
		GoodTests:        []string{"TestA"},
		ZeroKillTests:    []string{"TestB"},
		TestQualityScore: 50.0,
		AggregatedKills: map[string][]string{
			"TestA": {"mut1", "mut2"},
		},
	}

	t.Run("non-verbose JSON", func(t *testing.T) {
		data, err := FormatJSONReport(report, stats, false)
		if err != nil {
			t.Fatalf("FormatJSONReport failed: %v", err)
		}

		var parsed JSONReport
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("Unmarshal failed: %v", err)
		}

		if parsed.TotalMutations != 10 {
			t.Errorf("TotalMutations = %d, want 10", parsed.TotalMutations)
		}
		if parsed.Killed != 6 {
			t.Errorf("Killed = %d, want 6", parsed.Killed)
		}
		if parsed.Timeouts != 2 {
			t.Errorf("Timeouts = %d, want 2", parsed.Timeouts)
		}
		if parsed.Survived != 1 {
			t.Errorf("Survived = %d, want 1", parsed.Survived)
		}
		if parsed.Uncovered != 1 {
			t.Errorf("Uncovered = %d, want 1", parsed.Uncovered)
		}
		if parsed.BuildFailures != 1 {
			t.Errorf("BuildFailures = %d, want 1", parsed.BuildFailures)
		}
		if parsed.TotalTests != 2 {
			t.Errorf("TotalTests = %d, want 2", parsed.TotalTests)
		}
		if !reflect.DeepEqual(parsed.GoodTests, []string{"TestA"}) {
			t.Errorf("GoodTests = %v, want [TestA]", parsed.GoodTests)
		}
		if !reflect.DeepEqual(parsed.ZeroKillTests, []string{"TestB"}) {
			t.Errorf("ZeroKillTests = %v, want [TestB]", parsed.ZeroKillTests)
		}
		if parsed.MutationScore != 80.0 {
			t.Errorf("MutationScore = %f, want 80.0", parsed.MutationScore)
		}
		if parsed.TestQualityScore != 50.0 {
			t.Errorf("TestQualityScore = %f, want 50.0", parsed.TestQualityScore)
		}
		if len(parsed.TestKills) != 0 {
			t.Errorf("TestKills should be omitted in non-verbose mode, got %v", parsed.TestKills)
		}

		// Ensure test_kills is not in raw JSON string
		if strings.Contains(string(data), "test_kills") {
			t.Errorf("test_kills should not appear in non-verbose JSON output: %s", string(data))
		}
	})

	t.Run("verbose JSON", func(t *testing.T) {
		data, err := FormatJSONReport(report, stats, true)
		if err != nil {
			t.Fatalf("FormatJSONReport failed: %v", err)
		}

		var parsed JSONReport
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("Unmarshal failed: %v", err)
		}

		if len(parsed.TestKills) != 1 || !reflect.DeepEqual(parsed.TestKills["TestA"], []string{"mut1", "mut2"}) {
			t.Errorf("TestKills = %v, want map[TestA:[mut1 mut2]]", parsed.TestKills)
		}
		if !strings.Contains(string(data), "test_kills") {
			t.Errorf("test_kills should appear in verbose JSON output: %s", string(data))
		}
	})

	t.Run("nil report handling", func(t *testing.T) {
		emptyStats := TestStats{}
		data, err := FormatJSONReport(nil, emptyStats, false)
		if err != nil {
			t.Fatalf("FormatJSONReport failed: %v", err)
		}

		var parsed JSONReport
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("Unmarshal failed: %v", err)
		}

		if parsed.TotalMutations != 0 || parsed.MutationScore != 0.0 {
			t.Errorf("expected 0 for nil report, got %+v", parsed)
		}
		if parsed.GoodTests == nil || parsed.ZeroKillTests == nil {
			t.Errorf("expected non-nil empty slices for GoodTests/ZeroKillTests, got %+v", parsed)
		}
	})
}

func TestPrintHumanReport(t *testing.T) {
	report := &Report{
		Total:         10,
		Killed:        6,
		Timeouts:      2,
		Survived:      1,
		Uncovered:     1,
		BuildFailures: 1,
	}
	stats := TestStats{
		TotalTests:       3,
		GoodTests:        []string{"TestA", "TestB"},
		ZeroKillTests:    []string{"TestC"},
		TestQualityScore: 66.67,
		AggregatedKills: map[string][]string{
			"TestA": {"mut1", "mut2"},
			"TestB": {"mut3"},
		},
	}

	t.Run("non-verbose report output", func(t *testing.T) {
		var buf bytes.Buffer
		PrintHumanReport(&buf, report, stats, false)
		out := buf.String()

		expectedSnippets := []string{
			"Total mutations: 10",
			"Killed:          6",
			"Timeouts:        2",
			"Survived:        1",
			"Uncovered:       1",
			"Build Failures:  1",
			"Total tests:     3",
			"Good tests:      2",
			"Zero-kill tests: 1",
			"Zero-kill tests (caught 0 mutations):",
			"- TestC",
			"Mutation Score:     80.00% (killed/total mutations)",
			"Test Quality Score: 66.67% (good tests/total tests)",
		}

		for _, snippet := range expectedSnippets {
			if !strings.Contains(out, snippet) {
				t.Errorf("expected output to contain %q, but got:\n%s", snippet, out)
			}
		}

		if strings.Contains(out, "Good tests details:") {
			t.Errorf("non-verbose output should not contain Good tests details")
		}
	})

	t.Run("verbose report output", func(t *testing.T) {
		var buf bytes.Buffer
		PrintHumanReport(&buf, report, stats, true)
		out := buf.String()

		expectedSnippets := []string{
			"Good tests details:",
			"- TestA (caught 2 mutations): mut1, mut2",
			"- TestB (caught 1 mutations): mut3",
		}

		for _, snippet := range expectedSnippets {
			if !strings.Contains(out, snippet) {
				t.Errorf("expected output to contain %q, but got:\n%s", snippet, out)
			}
		}
	})

	t.Run("report without build failures or bad tests", func(t *testing.T) {
		cleanReport := &Report{
			Total:     5,
			Killed:    5,
			Survived:  0,
			Uncovered: 0,
		}
		cleanStats := TestStats{
			TotalTests:       1,
			GoodTests:        []string{"TestA"},
			ZeroKillTests:    []string{},
			TestQualityScore: 100.0,
			AggregatedKills: map[string][]string{
				"TestA": {"mut1"},
			},
		}

		var buf bytes.Buffer
		PrintHumanReport(&buf, cleanReport, cleanStats, false)
		out := buf.String()

		if strings.Contains(out, "Build Failures:") {
			t.Errorf("output should not contain Build Failures when 0")
		}
		if strings.Contains(out, "Zero-kill tests (caught 0 mutations):") {
			t.Errorf("output should not contain Zero-kill tests section when 0 zero-kill tests")
		}
	})

	t.Run("nil report", func(t *testing.T) {
		var buf bytes.Buffer
		PrintHumanReport(&buf, nil, TestStats{}, false)
		out := buf.String()

		if !strings.Contains(out, "Total mutations: 0") {
			t.Errorf("expected Total mutations: 0, got:\n%s", out)
		}
	})

	t.Run("safety-excluded report analytics", func(t *testing.T) {
		safetyReport := &Report{
			Total:     5,
			Killed:    2,
			Timeouts:  1,
			Survived:  1,
			Uncovered: 0,
			Excluded:  1,
			ExcludedList: []ExcludedMutant{
				{
					MutantID: "mut-safe-1",
					Mutator:  "string_literal",
					File:     "cleanup.go",
					Line:     15,
					Col:      8,
					Reason:   "argument to os.RemoveAll",
				},
			},
		}
		safetyStats := TestStats{
			TotalTests:       1,
			GoodTests:        []string{"TestA"},
			ZeroKillTests:    []string{},
			TestQualityScore: 100.0,
		}

		var buf bytes.Buffer
		PrintHumanReport(&buf, safetyReport, safetyStats, false)
		out := buf.String()

		if !strings.Contains(out, "Safety-excluded: 1") {
			t.Errorf("expected output to contain 'Safety-excluded: 1', got:\n%s", out)
		}
		if !strings.Contains(out, "- cleanup.go:15:8: string_literal (argument to os.RemoveAll)") {
			t.Errorf("expected output to contain excluded mutant details, got:\n%s", out)
		}

		// Verify JSON includes Excluded and ExcludedMutations
		jsonData, err := FormatJSONReport(safetyReport, safetyStats, false)
		if err != nil {
			t.Fatalf("FormatJSONReport failed: %v", err)
		}
		var parsed JSONReport
		if err := json.Unmarshal(jsonData, &parsed); err != nil {
			t.Fatalf("Unmarshal failed: %v", err)
		}
		if parsed.Excluded != 1 {
			t.Errorf("parsed.Excluded = %d, want 1", parsed.Excluded)
		}
		if len(parsed.ExcludedMutations) != 1 || parsed.ExcludedMutations[0].Reason != "argument to os.RemoveAll" {
			t.Errorf("parsed.ExcludedMutations = %+v, want reason 'argument to os.RemoveAll'", parsed.ExcludedMutations)
		}
	})
}
