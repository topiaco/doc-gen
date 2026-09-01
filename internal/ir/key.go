package ir

import (
	"fmt"
	"strings"
	"unicode"
)

// OperationKey 表示 Method 与规范化 Path 组成的精确键
type OperationKey struct {
	Method string
	Path   string
}

// NewOperationKey 从源码路由创建规范化精确键
func NewOperationKey(method, path string) (OperationKey, error) {
	if !isUpperMethod(method) {
		return OperationKey{}, fmt.Errorf("HTTP method %q must contain uppercase letters only", method)
	}

	normalizedPath, err := NormalizePath(path)
	if err != nil {
		return OperationKey{}, err
	}
	return OperationKey{Method: method, Path: normalizedPath}, nil
}

// ParseOperationKey 解析配置中的规范化精确键
func ParseOperationKey(raw string) (OperationKey, error) {
	method, path, ok := strings.Cut(raw, " ")
	if !ok || method == "" || path == "" || strings.Contains(path, " ") {
		return OperationKey{}, fmt.Errorf("operation key %q must use METHOD + single space + PATH", raw)
	}

	key, err := NewOperationKey(method, path)
	if err != nil {
		return OperationKey{}, err
	}
	if key.Path != path {
		return OperationKey{}, fmt.Errorf("operation path %q must be normalized as %q", path, key.Path)
	}
	return key, nil
}

// String 返回配置使用的精确键格式
func (k OperationKey) String() string {
	return k.Method + " " + k.Path
}

// NormalizePath 将 Gin 参数路径转换为 OpenAPI 路径
func NormalizePath(path string) (string, error) {
	if !strings.HasPrefix(path, "/") {
		return "", fmt.Errorf("path %q must start with /", path)
	}
	if strings.ContainsAny(path, "?#") || strings.IndexFunc(path, unicode.IsSpace) >= 0 {
		return "", fmt.Errorf("path %q must not contain query, fragment, or whitespace", path)
	}
	if strings.Contains(path, "//") {
		return "", fmt.Errorf("path %q contains an empty segment", path)
	}

	segments := strings.Split(path, "/")
	for i := 1; i < len(segments); i++ {
		segment := segments[i]
		if segment == "" {
			continue
		}

		switch segment[0] {
		case ':', '*':
			name := segment[1:]
			if !validParameterName(name) {
				return "", fmt.Errorf("path %q contains invalid parameter %q", path, segment)
			}
			segments[i] = "{" + name + "}"
		default:
			if strings.ContainsAny(segment, ":*") {
				return "", fmt.Errorf("path %q contains an invalid parameter segment", path)
			}
			if strings.ContainsAny(segment, "{}") && !validOpenAPIParameter(segment) {
				return "", fmt.Errorf("path %q contains invalid braces", path)
			}
		}
	}
	return strings.Join(segments, "/"), nil
}

// isUpperMethod 判断 Method 是否只包含大写英文字母
func isUpperMethod(method string) bool {
	if method == "" {
		return false
	}
	for _, r := range method {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return true
}

// validOpenAPIParameter 校验完整的 OpenAPI 路径参数片段
func validOpenAPIParameter(segment string) bool {
	return len(segment) > 2 && segment[0] == '{' && segment[len(segment)-1] == '}' && validParameterName(segment[1:len(segment)-1])
}

// validParameterName 校验路径参数名称
func validParameterName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if r == '_' || unicode.IsLetter(r) || i > 0 && unicode.IsDigit(r) {
			continue
		}
		return false
	}
	return true
}
