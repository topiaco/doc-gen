package ir

import (
	"fmt"
	"strings"
	"unicode"
)

// OperationKey 使用 HTTP 方法和规范化路径标识接口
type OperationKey struct {
	Method string
	Path   string
}

// String 返回 HTTP 方法、空格和规范化路径组成的字符串
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
