package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestDefaultCLIInitializesAndGeneratesOnlyNewEndpoints 验证初始化后默认命令只为新增接口生成文档
func TestDefaultCLIInitializesAndGeneratesOnlyNewEndpoints(t *testing.T) {
	root := t.TempDir()
	_, file, _, _ := runtime.Caller(0)
	stub, err := filepath.Abs(filepath.Join(filepath.Dir(file), "..", "..", "internal", "analyzer", "testdata", "routes", "gin"))
	if err != nil {
		t.Fatal(err)
	}
	module := "module cli/example\ngo 1.25.0\nrequire github.com/gin-gonic/gin v0.0.0\nreplace github.com/gin-gonic/gin => " + filepath.ToSlash(stub) + "\n"
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(module), 0o644); err != nil {
		t.Fatal(err)
	}
	original := `package example
import "github.com/gin-gonic/gin"
// Item 商品
type Item struct {Name string ` + "`json:\"name\"`" + ` /* 商品名称 */}
// Get 查询商品
func Get(c *gin.Context){c.JSON(200,Item{})}
func Register(e *gin.Engine){e.GET("/old",Get)}
`
	path := filepath.Join(root, "main.go")
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"init", "--existing"}, &stdout, &stderr); code != 0 {
		t.Fatalf("init %d: %s", code, stderr.String())
	}
	if code := run(nil, &stdout, &stderr); code != 0 {
		t.Fatalf("empty generate %d: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "operations=0") {
		t.Fatalf("existing: %s", stdout.String())
	}
	next := strings.Replace(original, `e.GET("/old",Get)`, `e.GET("/old",Get);e.GET("/new",Get)`, 1)
	if err := os.WriteFile(path, []byte(next), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := run(nil, &stdout, &stderr); code != 0 {
		t.Fatalf("default generate %d: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "operations=1") {
		t.Fatalf("new: %s", stdout.String())
	}
	data, err := os.ReadFile(filepath.Join(root, ".apidoc", "output", "openapi.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"/new"`)) || bytes.Contains(data, []byte(`"/old"`)) {
		t.Fatalf("selection: %s", data)
	}
	if code := run([]string{"validate"}, &stdout, &stderr); code != 0 {
		t.Fatalf("validate %d: %s", code, stderr.String())
	}
}
