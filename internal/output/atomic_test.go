package output

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWriteAtomicWritesAndReplacesFiles 验证多个生成产物完整写入并可稳定替换
func TestWriteAtomicWritesAndReplacesFiles(t *testing.T) {
	dir := t.TempDir()
	openAPIPath := filepath.Join(dir, "nested", "openapi.json")
	reportPath := filepath.Join(dir, "nested", "report.json")

	if err := WriteAtomic(
		File{Path: openAPIPath, Data: []byte("openapi-v1\n")},
		File{Path: reportPath, Data: []byte("report-v1\n")},
	); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(
		File{Path: openAPIPath, Data: []byte("openapi-v2\n")},
		File{Path: reportPath, Data: []byte("report-v2\n")},
	); err != nil {
		t.Fatal(err)
	}

	assertFileContent(t, openAPIPath, "openapi-v2\n")
	assertFileContent(t, reportPath, "report-v2\n")
	entries, err := os.ReadDir(filepath.Dir(openAPIPath))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("output directory contains %d files, want 2", len(entries))
	}
}

// assertFileContent 校验测试文件内容
func assertFileContent(t *testing.T, path, expected string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != expected {
		t.Fatalf("file %s = %q, want %q", path, data, expected)
	}
}
