package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/topiaco/doc-gen/internal/report"
)

// TestRunHelp 验证 CLI 帮助包含所有对外命令
func TestRunHelp(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exitCode := run(nil, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("run() exit code = %d, want 0", exitCode)
	}
	for _, command := range []string{"generate", "validate", "explain", "list", "list-unresolved", "check-overrides"} {
		if !strings.Contains(stdout.String(), command) {
			t.Errorf("help output does not contain %q", command)
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

// TestRunVersion 验证开发版本输出
func TestRunVersion(t *testing.T) {
	var stdout bytes.Buffer
	if exitCode := run([]string{"version"}, &stdout, &bytes.Buffer{}); exitCode != 0 {
		t.Fatalf("run() exit code = %d, want 0", exitCode)
	}
	if got, want := stdout.String(), "apidoc-gen dev\n"; got != want {
		t.Fatalf("version output = %q, want %q", got, want)
	}
}

// TestRunFixtureCommands 验证通用项目的只读命令
func TestRunFixtureCommands(t *testing.T) {
	root := fixtureProjectRoot(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if exitCode := run([]string{"list", "--project-root", root, "--mode", "managed"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("list exit code = %d, stderr = %q", exitCode, stderr.String())
	}
	if lines := strings.Split(strings.TrimSpace(stdout.String()), "\n"); len(lines) != 2 {
		t.Fatalf("managed operations = %q", stdout.String())
	}
	for _, key := range []string{"GET /catalog/{id}", "POST /catalog"} {
		if !strings.Contains(stdout.String(), key) {
			t.Fatalf("list output does not contain %q: %q", key, stdout.String())
		}
	}

	stdout.Reset()
	if exitCode := run([]string{"check-overrides", "--project-root", root}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("check-overrides exit code = %d, stderr = %q", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "summary\tvalid=0\twarnings=0\terrors=0") {
		t.Fatalf("check-overrides output = %q", stdout.String())
	}

	stdout.Reset()
	if exitCode := run([]string{"validate", "--project-root", root}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("validate exit code = %d, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "summary\troutes=2\tmanaged=2\tmanual=0\tignored=0\tgenerated=2\tunknownResponses=0") {
		t.Fatalf("validate output = %q", stdout.String())
	}

	stdout.Reset()
	if exitCode := run([]string{"explain", "--project-root", root, "GET /catalog/{id}"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("explain exit code = %d, stderr = %q", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "parameter\tpath\tid") {
		t.Fatalf("explain output = %q", stdout.String())
	}

	stdout.Reset()
	if exitCode := run([]string{"list-unresolved", "--project-root", root}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("list-unresolved exit code = %d, stderr = %q", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "summary\tunresolved=0") {
		t.Fatalf("list-unresolved output = %q", stdout.String())
	}
}

// TestRunGenerateWritesStableArtifacts 验证通用项目生成稳定产物
func TestRunGenerateWritesStableArtifacts(t *testing.T) {
	configPath, openAPIPath, reportPath := writeOutputConfig(t)
	args := []string{"generate", "--project-root", fixtureProjectRoot(t), "--config", configPath}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if exitCode := run(args, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("generate exit code = %d, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	firstOpenAPI := readTestFile(t, openAPIPath)
	firstReport := readTestFile(t, reportPath)
	var generatedReport report.Document
	if err := json.Unmarshal(firstReport, &generatedReport); err != nil {
		t.Fatal(err)
	}
	if generatedReport.Summary.DiscoveredRoutes != 2 || generatedReport.Summary.GeneratedOperations != 2 {
		t.Fatalf("report summary = %#v", generatedReport.Summary)
	}

	stdout.Reset()
	if exitCode := run(args, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("second generate exit code = %d, stderr = %q", exitCode, stderr.String())
	}
	if !bytes.Equal(firstOpenAPI, readTestFile(t, openAPIPath)) || !bytes.Equal(firstReport, readTestFile(t, reportPath)) {
		t.Fatal("repeated generate produced different artifact bytes")
	}
}

// TestRunRejectsInvalidArguments 验证 CLI 使用错误和配置错误退出码
func TestRunRejectsInvalidArguments(t *testing.T) {
	var stderr bytes.Buffer
	if exitCode := run([]string{"list", "--mode", "unknown"}, &bytes.Buffer{}, &stderr); exitCode != 2 {
		t.Fatalf("invalid mode exit code = %d, want 2", exitCode)
	}
	stderr.Reset()
	if exitCode := run([]string{"validate", "--project-root", fixtureProjectRoot(t), "--config", "missing.yaml"}, &bytes.Buffer{}, &stderr); exitCode != 1 {
		t.Fatalf("missing config exit code = %d, want 1", exitCode)
	}
}

func writeOutputConfig(t *testing.T) (configPath, openAPIPath, reportPath string) {
	t.Helper()
	root := fixtureProjectRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "api-doc", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	openAPIPath = filepath.Join(dir, "openapi.json")
	reportPath = filepath.Join(dir, "report.json")
	content := strings.Replace(string(data), "output: ./api-doc/openapi.json", "output: \""+openAPIPath+"\"", 1)
	content = strings.Replace(content, "report: ./api-doc/report.json", "report: \""+reportPath+"\"", 1)
	configPath = filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return configPath, openAPIPath, reportPath
}

func readTestFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func fixtureProjectRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate CLI test source")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", "testdata", "project"))
}
