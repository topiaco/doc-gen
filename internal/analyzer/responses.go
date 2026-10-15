package analyzer

import (
	"github.com/topiaco/doc-gen/internal/ir"
	"sort"
	"strconv"
)

// appendResponseContent 去重追加同一状态码的响应变体
func appendResponseContent(response *ir.Response, content ir.ResponseContent) {
	key := responseContentKey(content)
	for _, existing := range response.Content {
		if responseContentKey(existing) == key {
			return
		}
	}
	response.Content = append(response.Content, content)
}

// mergeOperationResponse 合并相同状态码的内容和重定向信息
func mergeOperationResponse(operation *ir.Operation, incoming ir.Response) {
	for i := range operation.Responses {
		if operation.Responses[i].Status != incoming.Status {
			continue
		}
		for _, content := range incoming.Content {
			appendResponseContent(&operation.Responses[i], content)
		}
		if incoming.RedirectConfidence != "" {
			operation.Responses[i].RedirectLocation = incoming.RedirectLocation
			operation.Responses[i].RedirectConfidence = incoming.RedirectConfidence
		}
		return
	}
	operation.Responses = append(operation.Responses, incoming)
}

// sortOperationResponses 稳定排序状态码和内容变体
func sortOperationResponses(operation *ir.Operation) {
	sort.Slice(operation.Responses, func(i, j int) bool {
		return operation.Responses[i].Status < operation.Responses[j].Status
	})
	for i := range operation.Responses {
		sort.Slice(operation.Responses[i].Content, func(left, right int) bool {
			return responseContentKey(operation.Responses[i].Content[left]) < responseContentKey(operation.Responses[i].Content[right])
		})
	}
}

// responseContentKey 返回响应变体的稳定去重键
func responseContentKey(content ir.ResponseContent) string {
	typeName := ""
	if content.Type != nil {
		typeName = content.Type.String()
	}
	return content.MediaType + "\x00" + typeName + "\x00" + strconv.FormatBool(content.Binary)
}
