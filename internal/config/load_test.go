package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadProjectConfig 验证仓库配置能够完整加载并解析路径
func TestLoadProjectConfig(t *testing.T) {
	projectRoot := filepath.Clean("../../testdata/project")
	bundle, err := Load(projectRoot, "api-doc/config.yaml")
	if err != nil {
		t.Fatal(err)
	}

	root, err := filepath.Abs(projectRoot)
	if err != nil {
		t.Fatal(err)
	}

	if bundle.Paths.ProjectRoot != root {
		t.Fatalf("project root = %q, want %q", bundle.Paths.ProjectRoot, root)
	}
	if bundle.Paths.Output != filepath.Join(root, "api-doc/openapi.json") {
		t.Fatalf("output path = %q", bundle.Paths.Output)
	}
	if bundle.Config.Project.Module != "example/catalog" {
		t.Fatalf("project module = %q, want example/catalog", bundle.Config.Project.Module)
	}
	if bundle.Selection.DefaultMode != "manual" {
		t.Fatalf("default mode = %q, want manual", bundle.Selection.DefaultMode)
	}
	if len(bundle.Overrides) != 1 || filepath.Base(bundle.Overrides[0].Path) != "base.yaml" {
		t.Fatalf("overrides = %#v, want base.yaml", bundle.Overrides)
	}
}

// TestLoadOverridesInFilenameOrder 验证 Override 文件按文件名稳定加载
func TestLoadOverridesInFilenameOrder(t *testing.T) {
	root := writeFixture(t, readProjectConfig(t), validSelection, validOverride)
	overrides := filepath.Join(root, "api-doc", "overrides")
	writeTestFile(t, filepath.Join(overrides, "z-last.yaml"), validOverride)
	writeTestFile(t, filepath.Join(overrides, "a-first.yaml"), validOverride)

	bundle, err := Load(root, "api-doc/config.yaml")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	actual := make([]string, len(bundle.Overrides))
	for i, file := range bundle.Overrides {
		actual[i] = filepath.Base(file.Path)
	}
	want := []string{"a-first.yaml", "base.yaml", "z-last.yaml"}
	if strings.Join(actual, ",") != strings.Join(want, ",") {
		t.Fatalf("override order = %v, want %v", actual, want)
	}
}

// TestLoadRejectsInvalidProjectConfig 验证主配置的版本、字段和重复键校验
func TestLoadRejectsInvalidProjectConfig(t *testing.T) {
	validConfig := readProjectConfig(t)
	tests := map[string]string{
		"invalid version":      strings.Replace(validConfig, "configVersion: v2", "configVersion: v1", 1),
		"unknown field":        validConfig + "\nunexpected: true\n",
		"duplicate key":        "configVersion: v2\n" + validConfig,
		"unknown wrapper":      strings.Replace(validConfig, "wrapper: envelope", "wrapper: missing", 1),
		"absolute file path":   strings.Replace(validConfig, "selection: selection.yaml", "selection: /tmp/selection.yaml", 1),
		"path outside project": strings.Replace(validConfig, "selection: selection.yaml", "selection: ../../selection.yaml", 1),
		"module mismatch":      strings.Replace(validConfig, "module: example/catalog", "module: example/other", 1),
		"conflicting status":   strings.Replace(validConfig, "status: 200\n    dataArgument", "status: 200\n    statusArgument: 0\n    dataArgument", 1),
	}

	for name, projectConfig := range tests {
		t.Run(name, func(t *testing.T) {
			root := writeFixture(t, projectConfig, validSelection, validOverride)
			_, err := Load(root, "api-doc/config.yaml")
			if err == nil {
				t.Fatal("Load() error = nil, want validation error")
			}
			if name == "invalid version" && !strings.Contains(err.Error(), "unsupported configVersion \"v1\"") {
				t.Fatalf("Load() error = %q, want unsupported v1 error", err)
			}
		})
	}
}

// TestLoadRejectsInvalidReferencedConfig 验证 Selection 和 Override 的文件边界
func TestLoadRejectsInvalidReferencedConfig(t *testing.T) {
	tests := []struct {
		name        string
		selection   string
		override    string
		mutate      func(t *testing.T, root string)
		wantInError string
	}{
		{
			name:        "missing selection",
			selection:   validSelection,
			override:    validOverride,
			mutate:      removeSelection,
			wantInError: "selection.yaml",
		},
		{
			name:        "unknown selection field",
			selection:   validSelection + "unexpected: true\n",
			override:    validOverride,
			wantInError: "selection.yaml",
		},
		{
			name:        "invalid selection version",
			selection:   strings.Replace(validSelection, "v2", "v1", 1),
			override:    validOverride,
			wantInError: "selection.yaml",
		},
		{
			name:        "invalid override version",
			selection:   validSelection,
			override:    strings.Replace(validOverride, "v2", "v1", 1),
			wantInError: "base.yaml",
		},
		{
			name:        "missing overrides directory",
			selection:   validSelection,
			override:    validOverride,
			mutate:      removeOverrides,
			wantInError: "overrides",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := writeFixture(t, readProjectConfig(t), test.selection, test.override)
			if test.mutate != nil {
				test.mutate(t, root)
			}

			_, err := Load(root, "api-doc/config.yaml")
			if err == nil {
				t.Fatal("Load() error = nil, want validation error")
			}
			if !strings.Contains(err.Error(), test.wantInError) {
				t.Fatalf("Load() error = %q, want path containing %q", err, test.wantInError)
			}
		})
	}
}

const validSelection = `configVersion: v2
defaultMode: manual
operations: {}
`

const validOverride = `configVersion: v2
operations: {}
schemas: {}
`

// readProjectConfig 读取仓库中的基准项目配置
func readProjectConfig(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("../../testdata/project/api-doc/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// writeFixture 写入一组可独立加载的配置夹具
func writeFixture(t *testing.T, projectConfig, selection, override string) string {
	t.Helper()
	root := t.TempDir()
	overridesDir := filepath.Join(root, "api-doc/overrides")
	if err := os.MkdirAll(overridesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(root, "go.mod"), "module example/catalog\n\ngo 1.25\n")
	writeTestFile(t, filepath.Join(root, "api-doc/config.yaml"), projectConfig)
	writeTestFile(t, filepath.Join(root, "api-doc/selection.yaml"), selection)
	writeTestFile(t, filepath.Join(overridesDir, "base.yaml"), override)
	return root
}

// writeTestFile 写入测试配置文件
func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// removeSelection 删除夹具中的 Selection 文件
func removeSelection(t *testing.T, root string) {
	t.Helper()
	if err := os.Remove(filepath.Join(root, "api-doc/selection.yaml")); err != nil {
		t.Fatal(err)
	}
}

// removeOverrides 删除夹具中的 Override 目录
func removeOverrides(t *testing.T, root string) {
	t.Helper()
	if err := os.RemoveAll(filepath.Join(root, "api-doc/overrides")); err != nil {
		t.Fatal(err)
	}
}
