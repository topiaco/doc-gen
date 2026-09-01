package sideeffect

import "os"

// init 写入标记文件以暴露任何运行时加载
func init() {
	marker := os.Getenv("APIDOC_INIT_MARKER")
	if marker != "" {
		_ = os.WriteFile(marker, []byte("executed"), 0o600)
	}
}

// Value 提供可供类型检查的包级声明
const Value = 42
