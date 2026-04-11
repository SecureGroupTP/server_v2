package clientapi

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
)

func normalizeLimit(limit int, fallback int) int {
	if limit <= 0 || limit > 200 {
		return fallback
	}
	return limit
}

func decodeOffsetCursor(cursor string) (int, error) {
	if strings.TrimSpace(cursor) == "" {
		return 0, nil
	}

	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, fmt.Errorf("invalid cursor: %w", err)
	}

	offset, err := strconv.Atoi(string(raw))
	if err != nil || offset < 0 {
		return 0, fmt.Errorf("invalid cursor")
	}

	return offset, nil
}

func encodeOffsetCursor(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(offset)))
}

func paginateSlice[T any](items []T, limit int, offset int) ([]T, any) {
	if len(items) <= limit {
		return items, nil
	}
	return items[:limit], encodeOffsetCursor(offset + limit)
}
