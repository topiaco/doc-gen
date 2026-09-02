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

// TestNewOperationKey 验证源码路由生成规范化精确键
func TestNewOperationKey(t *testing.T) {
	key, err := NewOperationKey("GET", "/widgets/:id")
	if err != nil {
		t.Fatalf("NewOperationKey() error = %v", err)
	}
	if got := key.String(); got != "GET /widgets/{id}" {
		t.Fatalf("OperationKey.String() = %q", got)
	}
}

// TestParseOperationKey 验证配置精确键只能使用规范化格式
func TestParseOperationKey(t *testing.T) {
	key, err := ParseOperationKey("PUT /widgets/{id}")
	if err != nil {
		t.Fatalf("ParseOperationKey() error = %v", err)
	}
	if key.Method != "PUT" || key.Path != "/widgets/{id}" {
		t.Fatalf("ParseOperationKey() = %#v", key)
	}
}

// TestParseOperationKeyRejectsInvalidKey 验证非精确键格式会被拒绝
func TestParseOperationKeyRejectsInvalidKey(t *testing.T) {
	keys := []string{
		"get /widgets/{id}",
		"GET  /widgets/{id}",
		"GET /widgets/:id",
		"GET widgets/{id}",
		"GET",
		"GET /widgets/{id} extra",
	}

	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			if _, err := ParseOperationKey(key); err == nil {
				t.Fatalf("ParseOperationKey(%q) should fail", key)
			}
		})
	}
}
