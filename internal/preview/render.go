// Package preview 将文档和诊断嵌入无需外部资源的离线页面
package preview

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
)

//go:embed index.html
var page string

// Render 序列化 JSON 时转义 HTML 特殊字符，因此源码文字不会被浏览器当作脚本执行
func Render(spec, report []byte) ([]byte, error) {
	encode := func(data []byte) (template.JS, error) {
		var value any
		if err := json.Unmarshal(data, &value); err != nil {
			return "", err
		}
		safe, err := json.Marshal(value)
		return template.JS(safe), err
	}
	api, err := encode(spec)
	if err != nil {
		return nil, fmt.Errorf("preview OpenAPI: %w", err)
	}
	diagnostics, err := encode(report)
	if err != nil {
		return nil, fmt.Errorf("preview report: %w", err)
	}
	t, err := template.New("api-reference").Parse(page)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := t.Execute(&out, struct{ Spec, Report template.JS }{api, diagnostics}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
