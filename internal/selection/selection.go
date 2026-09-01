// Package selection applies exact operation management policies
package selection

import (
	"fmt"
	"sort"
	"strings"

	"github.com/topiaco/doc-gen/internal/config"
	"github.com/topiaco/doc-gen/internal/ir"
)

// Apply 为每条已发现路由应用显式状态或默认状态
func Apply(operations []ir.Operation, policy config.Selection) ([]ir.Operation, error) {
	discovered := make(map[string]struct{}, len(operations))
	for _, operation := range operations {
		discovered[operation.Key().String()] = struct{}{}
	}

	configured := make(map[string]config.SelectionOperation, len(policy.Operations))
	keys := make([]string, 0, len(policy.Operations))
	for rawKey := range policy.Operations {
		keys = append(keys, rawKey)
	}
	sort.Strings(keys)

	var orphans []string
	for _, rawKey := range keys {
		key, err := ir.ParseOperationKey(rawKey)
		if err != nil {
			return nil, fmt.Errorf("selection operation %q: %w", rawKey, err)
		}
		if _, found := discovered[key.String()]; !found {
			orphans = append(orphans, key.String())
			continue
		}
		configured[key.String()] = policy.Operations[rawKey]
	}
	if len(orphans) != 0 {
		return nil, fmt.Errorf("orphan selection operations: %s", strings.Join(orphans, ", "))
	}

	result := make([]ir.Operation, len(operations))
	copy(result, operations)
	for i := range result {
		key := result[i].Key().String()
		if selected, found := configured[key]; found {
			result[i].Mode = ir.DocumentationMode(selected.Mode)
			result[i].ModeReason = selected.Reason
			continue
		}
		result[i].Mode = ir.DocumentationMode(policy.DefaultMode)
		result[i].ModeReason = "selection defaultMode"
	}
	return result, nil
}

// Filter 返回指定状态的路由并保留原有顺序
func Filter(operations []ir.Operation, mode ir.DocumentationMode) []ir.Operation {
	if mode == "" {
		return operations
	}
	filtered := make([]ir.Operation, 0, len(operations))
	for _, operation := range operations {
		if operation.Mode == mode {
			filtered = append(filtered, operation)
		}
	}
	return filtered
}
