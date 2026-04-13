package clientapi

import (
	"fmt"
	"strings"
	"time"
)

func profileToMap(record ProfileRecord) map[string]any {
	return map[string]any{
		"publicKey":   record.PublicKey,
		"username":    record.Username,
		"displayName": nullableString(record.DisplayName),
		"bio":         nullableString(record.Bio),
		"lastSeenAt":  record.LastSeenAt.UTC().Format(time.RFC3339Nano),
	}
}

func roomToMap(record ChatRoomRecord) map[string]any {
	return map[string]any{
		"roomId":      record.RoomID,
		"title":       record.Title,
		"description": nullableString(record.Description),
		"visibility":  int(record.Visibility),
		"stateId":     record.StateID,
	}
}

func invitationRecordsToMaps(records []ChatInvitationRecord) []map[string]any {
	items := make([]map[string]any, 0, len(records))
	for _, record := range records {
		items = append(items, map[string]any{
			"invitationId":     record.InvitationID,
			"roomId":           record.RoomID,
			"inviterPublicKey": record.InviterPublicKey,
			"inviteePublicKey": record.InviteePublicKey,
			"state":            int(record.State),
			"createdAt":        record.CreatedAt.UTC().Format(time.RFC3339Nano),
		})
	}
	return items
}

func parseTimeValue(value any) (time.Time, error) {
	switch typed := value.(type) {
	case time.Time:
		return typed.UTC(), nil
	case string:
		parsed, err := time.Parse(time.RFC3339Nano, typed)
		if err != nil {
			return time.Time{}, err
		}
		return parsed.UTC(), nil
	case int64:
		return time.UnixMicro(typed).UTC(), nil
	case int:
		return time.UnixMicro(int64(typed)).UTC(), nil
	case int32:
		return time.UnixMicro(int64(typed)).UTC(), nil
	case uint64:
		return time.UnixMicro(int64(typed)).UTC(), nil
	case uint32:
		return time.UnixMicro(int64(typed)).UTC(), nil
	case uint:
		return time.UnixMicro(int64(typed)).UTC(), nil
	default:
		return time.Time{}, fmt.Errorf("invalid time value")
	}
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func fallbackContentType(value string) string {
	if strings.TrimSpace(value) == "" {
		return "application/octet-stream"
	}
	return value
}
