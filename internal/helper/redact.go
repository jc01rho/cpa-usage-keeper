package helper

import (
	"strings"

	"cpa-usage-keeper/internal/entities"
)

const sensitiveValueMask = "*********"

// RedactSensitiveValue 使用统一格式隐藏前端展示中的敏感值：长值保留前 3 位和后 6 位，短值全隐藏。
func RedactSensitiveValue(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || trimmed == "unknown" {
		return "unknown"
	}
	runes := []rune(trimmed)
	if len(runes) <= 9 {
		return sensitiveValueMask
	}
	return string(runes[:3]) + sensitiveValueMask + string(runes[len(runes)-6:])
}

// CPAAPIKeyDisplayKey returns the full stored API key identity.
// Remote keeper-export rows contain a non-reversible akf1_ fingerprint here,
// while directly synced rows can contain the original downstream API key.
func CPAAPIKeyDisplayKey(row entities.CPAAPIKey) string {
	if strings.TrimSpace(row.APIKey) != "" {
		return strings.TrimSpace(row.APIKey)
	}
	return strings.TrimSpace(row.DisplayKey)
}

// CPAAPIKeyDisplayName returns the alias first and otherwise the full stored key identity.
func CPAAPIKeyDisplayName(row entities.CPAAPIKey) string {
	if strings.TrimSpace(row.KeyAlias) != "" {
		return strings.TrimSpace(row.KeyAlias)
	}
	return CPAAPIKeyDisplayKey(row)
}
