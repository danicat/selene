package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danicat/selene/internal/runner"
)

func setupCLITestProject(t *testing.T) (dir, srcPath string) {
	t.Helper()
	dir = t.TempDir()

	goMod := `module example.com/clitest
go 1.20
`
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0644); err != nil {
		t.Fatalf("failed to write go.mod: %v", err)
	}

	src := `package clitest

func Max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
`
	srcPath = filepath.Join(dir, "max.go")
	if err := os.WriteFile(srcPath, []byte(src), 0644); err != nil {
		t.Fatalf("failed to write max.go: %v", err)
	}

	testSrc := `package clitest

import "testing"

func TestMax(t *testing.T) {
	if Max(10, 5) != 10 {
		t.Fail()
	}
	if Max(3, 7) != 7 {
		t.Fail()
	}
}

func TestUnused(t *testing.T) {
	// does not assert
}
`
	testPath := filepath.Join(dir, "max_test.go")
	if err := os.WriteFile(testPath, []byte(testSrc), 0644); err != nil {
		t.Fatalf("failed to write max_test.go: %v", err)
	}

	return dir, srcPath
}

func TestCLI_EndToEnd_HumanReport(t *testing.T) {
	dir, srcPath := setupCLITestProject(t)

	config := runner.Config{
		Verbose:     false,
		MutationDir: filepath.Join(dir, "mut"),
		Mutators:    runner.DefaultMutators(),
		Workers:     2,
		Timeout:     5 * time.Second,
	}

	report, err := runner.Run([]string{srcPath}, config)
	if err != nil {
		t.Fatalf("runner.Run failed: %v", err)
	}

	targets, _, err := runner.ResolveTargets([]string{srcPath})
	if err != nil {
		t.Fatalf("ResolveTargets failed: %v", err)
	}

	var discoveredTests []string
	for _, target := range targets {
		lines, err := runner.DiscoverTests(target.Dir)
		if err == nil {
			discoveredTests = append(discoveredTests, lines...)
		}
	}

	stats := runner.CalculateTestStats(discoveredTests, report.TestKills)

	var buf bytes.Buffer
	runner.PrintHumanReport(&buf, report, stats, false)
	out := buf.String()

	if !strings.Contains(out, "Total mutations:") {
		t.Errorf("expected human report to contain 'Total mutations:', got:\n%s", out)
	}
	if !strings.Contains(out, "Mutation Score:") {
		t.Errorf("expected human report to contain 'Mutation Score:', got:\n%s", out)
	}
	if !strings.Contains(out, "Test Quality Score:") {
		t.Errorf("expected human report to contain 'Test Quality Score:', got:\n%s", out)
	}
	if !strings.Contains(out, "- TestUnused") {
		t.Errorf("expected Bad test '- TestUnused' in report:\n%s", out)
	}
}

func TestCLI_EndToEnd_JSONReport(t *testing.T) {
	dir, srcPath := setupCLITestProject(t)

	config := runner.Config{
		Verbose:     true,
		MutationDir: filepath.Join(dir, "mut"),
		Mutators:    runner.DefaultMutators(),
		Workers:     2,
		Timeout:     5 * time.Second,
	}

	report, err := runner.Run([]string{srcPath}, config)
	if err != nil {
		t.Fatalf("runner.Run failed: %v", err)
	}

	targets, _, err := runner.ResolveTargets([]string{srcPath})
	if err != nil {
		t.Fatalf("ResolveTargets failed: %v", err)
	}

	var discoveredTests []string
	for _, target := range targets {
		lines, err := runner.DiscoverTests(target.Dir)
		if err == nil {
			discoveredTests = append(discoveredTests, lines...)
		}
	}

	stats := runner.CalculateTestStats(discoveredTests, report.TestKills)

	data, err := runner.FormatJSONReport(report, stats, true)
	if err != nil {
		t.Fatalf("FormatJSONReport failed: %v", err)
	}

	var parsed runner.JSONReport
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("json.Unmarshal failed: %v\nJSON:\n%s", err, string(data))
	}

	if parsed.TotalMutations == 0 {
		t.Errorf("expected non-zero total mutations in JSON report")
	}
	if parsed.TestKills == nil {
		t.Errorf("expected test_kills in verbose JSON report")
	}
	if len(parsed.ZeroKillTests) != 1 || parsed.ZeroKillTests[0] != "TestUnused" {
		t.Errorf("expected ZeroKillTests [TestUnused], got %v", parsed.ZeroKillTests)
	}
}

func TestCLI_EndToEnd_ZeroMutations_Human(t *testing.T) {
	emptyReport := &runner.Report{}
	emptyStats := runner.CalculateTestStats([]string{"TestA", "TestB"}, nil)

	var buf bytes.Buffer
	runner.PrintHumanReport(&buf, emptyReport, emptyStats, false)
	out := buf.String()

	if !strings.Contains(out, "Total mutations: 0") {
		t.Errorf("expected 'Total mutations: 0', got:\n%s", out)
	}
	if !strings.Contains(out, "Mutation Score:     0.00%") {
		t.Errorf("expected 'Mutation Score:     0.00%%%%', got:\n%s", out)
	}
	if !strings.Contains(out, "Zero-kill tests (caught 0 mutations):") {
		t.Errorf("expected Zero-kill tests section, got:\n%s", out)
	}
}

func TestCLI_EndToEnd_ZeroMutations_JSON(t *testing.T) {
	emptyReport := &runner.Report{}
	emptyStats := runner.CalculateTestStats(nil, nil)

	data, err := runner.FormatJSONReport(emptyReport, emptyStats, false)
	if err != nil {
		t.Fatalf("FormatJSONReport failed: %v", err)
	}

	var parsed runner.JSONReport
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if parsed.TotalMutations != 0 {
		t.Errorf("expected TotalMutations 0, got %d", parsed.TotalMutations)
	}
	if parsed.MutationScore != 0.0 {
		t.Errorf("expected MutationScore 0, got %f", parsed.MutationScore)
	}
	if parsed.GoodTests == nil || parsed.ZeroKillTests == nil {
		t.Errorf("expected non-nil empty slices in JSON, got %+v", parsed)
	}
}

func TestCLI_EndToEnd_ZeroKillTestsClassification(t *testing.T) {
	report := &runner.Report{
		Total:    4,
		Killed:   2,
		Survived: 2,
		TestKills: map[string][]string{
			"TestEffective": {"mut1", "mut2"},
		},
	}
	discoveredTests := []string{"TestEffective", "TestIneffective"}
	stats := runner.CalculateTestStats(discoveredTests, report.TestKills)

	if len(stats.GoodTests) != 1 || stats.GoodTests[0] != "TestEffective" {
		t.Errorf("expected GoodTests [TestEffective], got %v", stats.GoodTests)
	}
	if len(stats.ZeroKillTests) != 1 || stats.ZeroKillTests[0] != "TestIneffective" {
		t.Errorf("expected ZeroKillTests [TestIneffective], got %v", stats.ZeroKillTests)
	}
	if stats.TestQualityScore != 50.0 {
		t.Errorf("expected TestQualityScore 50.0, got %f", stats.TestQualityScore)
	}

	var buf bytes.Buffer
	runner.PrintHumanReport(&buf, report, stats, false)
	out := buf.String()

	if !strings.Contains(out, "Zero-kill tests (caught 0 mutations):") {
		t.Errorf("expected Zero-kill tests section in output: %s", out)
	}
	if !strings.Contains(out, "- TestIneffective") {
		t.Errorf("expected '- TestIneffective' in output: %s", out)
	}
}

func TestCLI_Version(t *testing.T) {
	if version == "" {
		t.Errorf("expected default version string to be non-empty")
	}
}

func TestCLI_EndToEnd_TimeboxFlag(t *testing.T) {
	d, err := time.ParseDuration("5m")
	if err != nil || d != 5*time.Minute {
		t.Fatalf("expected 5m, got %v (err: %v)", d, err)
	}

	d, err = time.ParseDuration("30s")
	if err != nil || d != 30*time.Second {
		t.Fatalf("expected 30s, got %v (err: %v)", d, err)
	}

	_, err = time.ParseDuration("invalid")
	if err == nil {
		t.Fatalf("expected error for invalid duration, got nil")
	}
}
