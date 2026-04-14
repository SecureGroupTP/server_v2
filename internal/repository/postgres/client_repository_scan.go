package postgres

import (
	"database/sql"

	clientapi "server_v2/internal/application/clientapi"
)

func scanProfiles(rows *sql.Rows) ([]clientapi.ProfileRecord, error) {
	var items []clientapi.ProfileRecord
	for rows.Next() {
		var item clientapi.ProfileRecord
		if err := rows.Scan(&item.PublicKey, &item.Username, &item.DisplayName, &item.Bio, &item.AvatarHash, &item.AvatarBytes, &item.ContentType, &item.LastSeenAt, &item.UpdatedAt, &item.DeletedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanRooms(rows *sql.Rows) ([]clientapi.ChatRoomRecord, error) {
	var items []clientapi.ChatRoomRecord
	for rows.Next() {
		var item clientapi.ChatRoomRecord
		if err := rows.Scan(&item.RoomID, &item.OwnerPublicKey, &item.Title, &item.Description, &item.Visibility, &item.AvatarHash, &item.AvatarBytes, &item.AvatarContentType, &item.StateID, &item.CreatedAt, &item.UpdatedAt, &item.DeletedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanInvitations(rows *sql.Rows) ([]clientapi.ChatInvitationRecord, error) {
	var items []clientapi.ChatInvitationRecord
	for rows.Next() {
		var item clientapi.ChatInvitationRecord
		if err := rows.Scan(&item.InvitationID, &item.RoomID, &item.InviterPublicKey, &item.InviteePublicKey, &item.ExpiresAt, &item.InviteToken, &item.InviteTokenSignature, &item.State, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
