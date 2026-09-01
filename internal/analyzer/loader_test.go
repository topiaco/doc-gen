package analyzer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestLoadDoesNotRunInit 验证静态加载不会执行目标包 init
func TestLoadDoesNotRunInit(t *testing.T) {
	fixtureRoot := filepath.Join(packageDir(t), "testdata", "sideeffect")
	marker := filepath.Join(t.TempDir(), "init-ran")
	t.Setenv("APIDOC_INIT_MARKER", marker)

	program, err := Load(context.Background(), fixtureRoot, []string{"./..."})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if program.Package("fixture/sideeffect") == nil {
		t.Fatal("fixture package was not loaded")
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("target package init executed, marker error = %v", err)
	}
}

// packageDir 返回当前测试包目录
func packageDir(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate loader test source")
	}
	return filepath.Dir(filename)
}
