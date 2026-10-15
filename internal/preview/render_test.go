package preview

import (
	"bytes"
	"encoding/json"
	"testing"
)

// TestOfflinePageEmbedsDataWithoutExecutableSourceText 验证页面可以离线使用，并将源码文字作为数据处理
func TestOfflinePageEmbedsDataWithoutExecutableSourceText(t *testing.T) {
	spec, _ := json.Marshal(map[string]any{"info": map[string]string{"title": "</script><script>alert(1)</script>"}, "paths": map[string]any{}})
	report := []byte(`{"summary":{"generated":0,"excluded":0,"warnings":0},"issues":[],"operations":[]}`)
	html, err := Render(spec, report)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(html, []byte("</script><script>alert(1)")) {
		t.Fatal("source comment escaped its JSON script element")
	}
	if bytes.Contains(html, []byte("<script src=")) || bytes.Contains(html, []byte("fetch(")) {
		t.Fatal("preview requires an external resource")
	}
	if !bytes.Contains(html, []byte(`\u003c/script\u003e`)) {
		t.Fatal("embedded source was not safely serialized")
	}
}
