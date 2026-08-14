package usecase

import (
	"encoding/json"

	"mailpulse/internal/entity"
)

func ptr[T any](value T) *T {
	return &value
}

func nilIfEmpty(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// jsonOrDefault keeps a not-null jsonb column valid when the caller sent nothing.
func jsonOrDefault(raw json.RawMessage, fallback string) entity.JSON {
	if len(raw) == 0 {
		return entity.JSON(fallback)
	}
	return entity.JSON(raw)
}

func truncate(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max])
}
