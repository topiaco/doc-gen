package ir

import "testing"

// TestNormalizePath 验证 Gin 路径向 OpenAPI 路径的转换
func TestNormalizePath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "named parameter", path: "/widgets/:id", want: "/widgets/{id}"},
		{name: "wildcard parameter", path: "/files/*path", want: "/files/{path}"},
		{name: "normalized parameter", path: "/widgets/{widget_id}", want: "/widgets/{widget_id}"},
		{name: "root", path: "/", want: "/"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := NormalizePath(test.path)
			if err != nil {
				t.Fatalf("NormalizePath() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("NormalizePath() = %q, want %q", got, test.want)
			}
		})
	}
}

// TestNormalizePathRejectsInvalidPath 验证非法路由路径会被拒绝
func TestNormalizePathRejectsInvalidPath(t *testing.T) {
	paths := []string{
		"widgets/:id",
		"/api//widgets",
		"/widgets/:",
		"/widgets/id:part",
		"/widgets/{id",
		"/widgets/{1id}",
		"/widgets/:id?active=true",
		"/widgets/#latest",
		"/widgets/with space",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			if _, err := NormalizePath(path); err == nil {
				t.Fatalf("NormalizePath(%q) should fail", path)
			}
		})
	}
}
