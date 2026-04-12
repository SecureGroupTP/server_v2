package clientapi

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	domainauth "server_v2/internal/domain/auth"
	domaintx "server_v2/internal/domain/tx"
)

type Config struct {
	AppName             string
	Version             string
	SessionChallengeTTL time.Duration
	EventRetention      time.Duration
	EventBatchSize      int
}

type Service struct {
	cfg       Config
	clock     Clock
	uuidGen   UUIDGenerator
	txManager domaintx.Manager
	store     Store
	events    EventAppender
	sessions  SessionLookup
}

func NewService(cfg Config, clock Clock, uuidGen UUIDGenerator, txManager domaintx.Manager, store Store, events EventAppender, sessions SessionLookup) (*Service, error) {
	if clock == nil || uuidGen == nil || txManager == nil || store == nil || events == nil || sessions == nil {
		return nil, fmt.Errorf("all dependencies are required")
	}
	if cfg.Version == "" {
		cfg.Version = "2"
	}
	if cfg.EventRetention <= 0 {
		cfg.EventRetention = 24 * time.Hour
	}
	if cfg.EventBatchSize <= 0 {
		cfg.EventBatchSize = 100
	}
	return &Service{cfg: cfg, clock: clock, uuidGen: uuidGen, txManager: txManager, store: store, events: events, sessions: sessions}, nil
}

func (s *Service) GetProfile(ctx context.Context, target []byte) (map[string]any, error) {
	if len(target) != ed25519.PublicKeySize {
		return nil, domainauth.ErrInvalidPublicKey
	}
	record, err := s.store.GetProfile(ctx, target)
	if err != nil {
		return nil, err
	}
	banStatus, found, err := s.store.GetActiveBanStatus(ctx, target, s.clock.Now())
	if err != nil {
		return nil, err
	}
	var banStatusValue any
	if found {
		banStatusValue = map[string]any{
			"isBanned": banStatus.IsBanned,
			"reason":   nullableString(banStatus.Reason),
		}
	}
	return map[string]any{
		"profile":   profileToMap(record),
		"banStatus": banStatusValue,
	}, nil
}

func (s *Service) UpdateProfile(ctx context.Context, user []byte, displayName string, avatarHash string, bio string) (map[string]any, error) {
	now := s.clock.Now()
	if err := s.store.UpdateProfile(ctx, user, displayName, avatarHash, bio, now); err != nil {
		return nil, err
	}
	limit := s.cfg.EventBatchSize
	for offset := 0; ; offset += limit {
		friends, err := s.store.ListFriends(ctx, user, limit, offset)
		if err != nil {
			return nil, err
		}
		for _, friend := range friends {
			_ = s.appendEvent(ctx, friend.FriendPublicKey, "profile.updated", map[string]any{
				"userPublicKey": user,
				"displayName":   displayName,
				"avatarHash":    avatarHash,
				"bio":           bio,
				"updatedAt":     now.UTC().Format(time.RFC3339Nano),
			})
		}
		if len(friends) < limit {
			break
		}
	}
	return map[string]any{"updatedAt": now.UTC().Format(time.RFC3339Nano)}, nil
}

func (s *Service) SearchProfiles(ctx context.Context, query string, limit int, cursor string) (map[string]any, error) {
	limit = normalizeLimit(limit, 20)
	offset, err := decodeOffsetCursor(cursor)
	if err != nil {
		return nil, err
	}
	records, err := s.store.SearchProfiles(ctx, query, limit+1, offset)
	if err != nil {
		return nil, err
	}
	records, nextCursor := paginateSlice(records, limit, offset)
	items := make([]map[string]any, 0, len(records))
	for _, record := range records {
		items = append(items, map[string]any{
			"publicKey":   record.PublicKey,
			"username":    record.Username,
			"displayName": nullableString(record.DisplayName),
		})
	}
	return map[string]any{"items": items, "nextCursor": nextCursor}, nil
}

func (s *Service) DeleteAccount(ctx context.Context, user []byte) (map[string]any, error) {
	now := s.clock.Now()
	if err := s.txManager.WithinTransaction(ctx, func(txCtx context.Context) error {
		return s.store.DeleteAccount(txCtx, user, now)
	}); err != nil {
		return nil, err
	}
	return map[string]any{"deletedAt": now.UTC().Format(time.RFC3339Nano)}, nil
}

func (s *Service) GetProfileAvatar(ctx context.Context, user []byte) (map[string]any, error) {
	avatar, err := s.store.GetProfileAvatar(ctx, user)
	if err != nil {
		return nil, err
	}
	return map[string]any{"avatarBytes": avatar.Bytes, "contentType": fallbackContentType(avatar.ContentType)}, nil
}

func (s *Service) ListDevices(ctx context.Context, user []byte) (map[string]any, error) {
	devices, err := s.store.ListDevices(ctx, user)
	if err != nil {
		return nil, err
	}
	items := make([]map[string]any, 0, len(devices))
	for _, device := range devices {
		items = append(items, map[string]any{
			"deviceId":  device.ID,
			"platform":  int(device.Platform),
			"pushToken": device.PushToken,
			"isEnabled": device.IsEnabled,
			"updatedAt": device.UpdatedAt.UTC().Format(time.RFC3339Nano),
		})
	}
	return map[string]any{"items": items}, nil
}

func (s *Service) RegisterDevicePushToken(ctx context.Context, sessionID uuid.UUID, user []byte, platform int16, pushToken string, isEnabled bool) (map[string]any, error) {
	session, err := s.sessions.LookupSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	now := s.clock.Now()
	device, err := s.store.UpsertDevice(ctx, DeviceRecord{
		ID:            s.uuidGen.New(),
		SessionID:     &sessionID,
		UserPublicKey: user,
		DeviceID:      session.DeviceID,
		Platform:      platform,
		PushToken:     pushToken,
		IsEnabled:     isEnabled,
		UpdatedAt:     now,
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"id": device.ID, "updatedAt": now.UTC().Format(time.RFC3339Nano)}, nil
}

func (s *Service) RemoveDevice(ctx context.Context, user []byte, deviceID uuid.UUID) (map[string]any, error) {
	now := s.clock.Now()
	if err := s.store.RemoveDevice(ctx, user, deviceID, now); err != nil {
		return nil, err
	}
	return map[string]any{"removedAt": now.UTC().Format(time.RFC3339Nano)}, nil
}

func (s *Service) UploadKeyPackages(ctx context.Context, sessionID uuid.UUID, user []byte, packages []map[string]any) (map[string]any, error) {
	session, err := s.sessions.LookupSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	now := s.clock.Now()
	records := make([]KeyPackageRecord, 0, len(packages))
	for _, item := range packages {
		expiresAt, err := parseTimeValue(item["expiresAt"])
		if err != nil {
			return nil, err
		}
		bytesValue, ok := item["keyPackageBytes"].([]byte)
		if !ok {
			return nil, fmt.Errorf("keyPackageBytes is required")
		}
		isLastResort, _ := item["isLastResort"].(bool)
		records = append(records, KeyPackageRecord{
			ID:              s.uuidGen.New(),
			UserPublicKey:   user,
			DeviceID:        session.DeviceID,
			KeyPackageBytes: bytesValue,
			IsLastResort:    isLastResort,
			CreatedAt:       now,
			ExpiresAt:       expiresAt,
		})
	}
	recordedCount, err := s.store.InsertKeyPackages(ctx, records)
	if err != nil {
		return nil, err
	}
	return map[string]any{"recordedCount": recordedCount}, nil
}

func (s *Service) FetchKeyPackages(ctx context.Context, userPublicKeys [][]byte) (map[string]any, error) {
	records, err := s.store.FetchKeyPackages(ctx, userPublicKeys, s.clock.Now())
	if err != nil {
		return nil, err
	}
	items := make([]map[string]any, 0, len(records))
	for _, record := range records {
		items = append(items, map[string]any{
			"userPublicKey":   record.UserPublicKey,
			"deviceId":        record.DeviceID,
			"keyPackageBytes": record.KeyPackageBytes,
		})
	}
	return map[string]any{"items": items}, nil
}

func (s *Service) ListFriends(ctx context.Context, user []byte, limit int, cursor string) (map[string]any, error) {
	limit = normalizeLimit(limit, 50)
	offset, err := decodeOffsetCursor(cursor)
	if err != nil {
		return nil, err
	}
	friends, err := s.store.ListFriends(ctx, user, limit+1, offset)
	if err != nil {
		return nil, err
	}
	totalCount, err := s.store.CountFriends(ctx, user)
	if err != nil {
		return nil, err
	}
	friends, nextCursor := paginateSlice(friends, limit, offset)
	items := make([]map[string]any, 0, len(friends))
	for _, item := range friends {
		items = append(items, map[string]any{
			"id":         item.ID,
			"friendId":   item.FriendPublicKey,
			"acceptedAt": item.AcceptedAt.UTC().Format(time.RFC3339Nano),
		})
	}
	return map[string]any{"items": items, "nextCursor": nextCursor, "totalCount": totalCount}, nil
}

func (s *Service) RemoveFriend(ctx context.Context, user []byte, friendPublicKey []byte) (map[string]any, error) {
	now := s.clock.Now()
	if err := s.store.RemoveFriend(ctx, user, friendPublicKey, now); err != nil {
		return nil, err
	}
	_ = s.appendEvent(ctx, friendPublicKey, "friend.removed", map[string]any{"userPublicKey": user})
	return map[string]any{"removedAt": now.UTC().Format(time.RFC3339Nano)}, nil
}

func (s *Service) SendFriendRequest(ctx context.Context, user []byte, receiver []byte) (map[string]any, error) {
	if len(receiver) != ed25519.PublicKeySize {
		return nil, domainauth.ErrInvalidPublicKey
	}
	now := s.clock.Now()
	record := FriendRequestRecord{
		RequestID:         s.uuidGen.New(),
		SenderPublicKey:   user,
		ReceiverPublicKey: receiver,
		State:             FriendRequestPending,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := s.store.CreateFriendRequest(ctx, record); err != nil {
		return nil, err
	}
	_ = s.appendEvent(ctx, receiver, "friend.requestReceived", map[string]any{"requestId": record.RequestID.String(), "senderPublicKey": user})
	return map[string]any{"requestId": record.RequestID, "state": int(record.State), "createdAt": now.UTC().Format(time.RFC3339Nano)}, nil
}

func (s *Service) AcceptFriendRequest(ctx context.Context, user []byte, requestID uuid.UUID) (map[string]any, error) {
	now := s.clock.Now()
	friendID := s.uuidGen.New()
	var record FriendRequestRecord
	if err := s.txManager.WithinTransaction(ctx, func(txCtx context.Context) error {
		var txErr error
		record, txErr = s.store.UpdateFriendRequestState(txCtx, requestID, user, []int16{FriendRequestPending}, FriendRequestAccepted, now)
		if txErr != nil {
			return txErr
		}
		if err := s.store.CreateFriendPair(txCtx,
			FriendRecord{ID: friendID, UserPublicKey: record.SenderPublicKey, FriendPublicKey: record.ReceiverPublicKey, AcceptedAt: now},
			FriendRecord{ID: s.uuidGen.New(), UserPublicKey: record.ReceiverPublicKey, FriendPublicKey: record.SenderPublicKey, AcceptedAt: now},
		); err != nil {
			return err
		}
		return s.appendEvent(txCtx, record.SenderPublicKey, "friend.requestAccepted", map[string]any{"requestId": requestID.String(), "friendPublicKey": user})
	}); err != nil {
		return nil, err
	}
	return map[string]any{"friendId": friendID, "acceptedAt": now.UTC().Format(time.RFC3339Nano)}, nil
}

func (s *Service) DeclineFriendRequest(ctx context.Context, user []byte, requestID uuid.UUID) (map[string]any, error) {
	now := s.clock.Now()
	record, err := s.store.UpdateFriendRequestState(ctx, requestID, user, []int16{FriendRequestPending}, FriendRequestDeclined, now)
	if err != nil {
		return nil, err
	}
	_ = s.appendEvent(ctx, record.SenderPublicKey, "friend.requestDeclined", map[string]any{"requestId": requestID.String()})
	return map[string]any{"requestId": requestID, "declinedAt": now.UTC().Format(time.RFC3339Nano)}, nil
}

func (s *Service) CancelFriendRequest(ctx context.Context, user []byte, requestID uuid.UUID) (map[string]any, error) {
	now := s.clock.Now()
	record, err := s.store.UpdateFriendRequestState(ctx, requestID, user, []int16{FriendRequestPending}, FriendRequestCanceled, now)
	if err != nil {
		return nil, err
	}
	_ = s.appendEvent(ctx, record.ReceiverPublicKey, "friend.requestCanceled", map[string]any{"requestId": requestID.String()})
	return map[string]any{"canceledAt": now.UTC().Format(time.RFC3339Nano)}, nil
}

func (s *Service) ListFriendRequests(ctx context.Context, user []byte, direction string, limit int, cursor string) (map[string]any, error) {
	limit = normalizeLimit(limit, 50)
	offset, err := decodeOffsetCursor(cursor)
	if err != nil {
		return nil, err
	}
	records, err := s.store.ListFriendRequests(ctx, user, direction, limit+1, offset)
	if err != nil {
		return nil, err
	}
	records, nextCursor := paginateSlice(records, limit, offset)
	items := make([]map[string]any, 0, len(records))
	for _, record := range records {
		items = append(items, map[string]any{
			"requestId":         record.RequestID,
			"senderPublicKey":   record.SenderPublicKey,
			"receiverPublicKey": record.ReceiverPublicKey,
			"state":             int(record.State),
		})
	}
	return map[string]any{"items": items, "nextCursor": nextCursor}, nil
}

func (s *Service) CreateChatRoom(ctx context.Context, user []byte, title string, description string, visibility int16) (map[string]any, error) {
	now := s.clock.Now()
	roomID := s.uuidGen.New()
	room := ChatRoomRecord{RoomID: roomID, OwnerPublicKey: user, Title: title, Description: description, Visibility: visibility, CreatedAt: now, UpdatedAt: now}
	owner := ChatMemberRecord{RoomID: roomID, UserPublicKey: user, Role: RoleOwner, NotificationLevel: NotificationAll, JoinedAt: now}
	if err := s.txManager.WithinTransaction(ctx, func(txCtx context.Context) error {
		return s.store.CreateRoom(txCtx, room, owner)
	}); err != nil {
		return nil, err
	}
	return map[string]any{"roomId": roomID, "createdAt": now.UTC().Format(time.RFC3339Nano)}, nil
}

func (s *Service) ListChatRooms(ctx context.Context, user []byte, limit int, cursor string) (map[string]any, error) {
	limit = normalizeLimit(limit, 50)
	offset, err := decodeOffsetCursor(cursor)
	if err != nil {
		return nil, err
	}
	rooms, err := s.store.ListRooms(ctx, user, limit+1, offset)
	if err != nil {
		return nil, err
	}
	rooms, nextCursor := paginateSlice(rooms, limit, offset)
	items := make([]map[string]any, 0, len(rooms))
	for _, room := range rooms {
		items = append(items, map[string]any{"roomId": room.RoomID, "title": room.Title, "updatedAt": room.UpdatedAt.UTC().Format(time.RFC3339Nano)})
	}
	return map[string]any{"items": items, "nextCursor": nextCursor}, nil
}

func (s *Service) GetChatRoom(ctx context.Context, roomID uuid.UUID) (map[string]any, error) {
	room, err := s.store.GetRoom(ctx, roomID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"room": roomToMap(room)}, nil
}

func (s *Service) SearchChatRooms(ctx context.Context, query string, limit int, cursor string) (map[string]any, error) {
	limit = normalizeLimit(limit, 50)
	offset, err := decodeOffsetCursor(cursor)
	if err != nil {
		return nil, err
	}
	rooms, err := s.store.SearchRooms(ctx, query, limit+1, offset)
	if err != nil {
		return nil, err
	}
	rooms, nextCursor := paginateSlice(rooms, limit, offset)
	items := make([]map[string]any, 0, len(rooms))
	for _, room := range rooms {
		items = append(items, map[string]any{"roomId": room.RoomID, "title": room.Title, "updatedAt": room.UpdatedAt.UTC().Format(time.RFC3339Nano)})
	}
	return map[string]any{"items": items, "nextCursor": nextCursor}, nil
}

func (s *Service) SyncChatRoom(ctx context.Context, roomID uuid.UUID) (map[string]any, error) {
	room, err := s.store.GetRoom(ctx, roomID)
	if err != nil {
		return nil, err
	}
	now := s.clock.Now()
	return map[string]any{"room": roomToMap(room), "syncedAt": now.UTC().Format(time.RFC3339Nano)}, nil
}

func (s *Service) UpdateChatRoom(ctx context.Context, user []byte, roomID uuid.UUID, title string, description string, avatarHash string) (map[string]any, error) {
	now := s.clock.Now()
	if err := s.store.UpdateRoom(ctx, user, roomID, title, description, avatarHash, now); err != nil {
		return nil, err
	}
	return map[string]any{"updatedAt": now.UTC().Format(time.RFC3339Nano)}, nil
}

func (s *Service) UpdateChatRoomState(ctx context.Context, user []byte, roomID uuid.UUID, groupID uuid.UUID, epoch int64, treeBytes []byte, treeHash []byte) (map[string]any, error) {
	now := s.clock.Now()
	state := ChatRoomStateRecord{ID: s.uuidGen.New(), RoomID: roomID, GroupID: groupID, Epoch: epoch, TreeBytes: treeBytes, TreeHash: treeHash, CreatedAt: now}
	if err := s.txManager.WithinTransaction(ctx, func(txCtx context.Context) error {
		return s.store.AddRoomState(txCtx, user, state)
	}); err != nil {
		return nil, err
	}
	return map[string]any{"acceptedAt": now.UTC().Format(time.RFC3339Nano)}, nil
}

func (s *Service) FetchChatRoomState(ctx context.Context, user []byte, roomID uuid.UUID, epoch int64) (map[string]any, error) {
	state, err := s.store.FetchRoomState(ctx, user, roomID, epoch)
	if err != nil {
		return nil, err
	}
	return map[string]any{"groupId": state.GroupID, "epoch": state.Epoch, "treeBytes": state.TreeBytes, "treeHash": state.TreeHash}, nil
}

func (s *Service) DeleteChatRoom(ctx context.Context, user []byte, roomID uuid.UUID) (map[string]any, error) {
	now := s.clock.Now()
	members, _ := s.store.ListActiveRoomMemberPublicKeys(ctx, roomID)
	if err := s.store.DeleteRoom(ctx, user, roomID, now); err != nil {
		return nil, err
	}
	for _, member := range members {
		_ = s.appendEvent(ctx, member, "room.deleted", map[string]any{"roomId": roomID.String()})
	}
	return map[string]any{"deletedAt": now.UTC().Format(time.RFC3339Nano)}, nil
}

func (s *Service) GetChatRoomAvatar(ctx context.Context, roomID uuid.UUID) (map[string]any, error) {
	avatar, err := s.store.GetRoomAvatar(ctx, roomID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"avatarBytes": avatar.Bytes, "contentType": fallbackContentType(avatar.ContentType)}, nil
}

func (s *Service) JoinChatRoom(ctx context.Context, user []byte, roomID uuid.UUID) (map[string]any, error) {
	now := s.clock.Now()
	if err := s.store.JoinRoom(ctx, ChatMemberRecord{RoomID: roomID, UserPublicKey: user, Role: RoleMember, NotificationLevel: NotificationAll, JoinedAt: now}); err != nil {
		return nil, err
	}
	members, _ := s.store.ListActiveRoomMemberPublicKeys(ctx, roomID)
	for _, member := range members {
		_ = s.appendEvent(ctx, member, "room.memberJoined", map[string]any{"roomId": roomID.String(), "userPublicKey": user})
	}
	return map[string]any{"joinedAt": now.UTC().Format(time.RFC3339Nano)}, nil
}

func (s *Service) LeaveChatRoom(ctx context.Context, user []byte, roomID uuid.UUID) (map[string]any, error) {
	now := s.clock.Now()
	if err := s.store.LeaveRoom(ctx, roomID, user, now); err != nil {
		return nil, err
	}
	members, _ := s.store.ListActiveRoomMemberPublicKeys(ctx, roomID)
	for _, member := range members {
		_ = s.appendEvent(ctx, member, "room.memberLeft", map[string]any{"roomId": roomID.String(), "userPublicKey": user})
	}
	return map[string]any{"leftAt": now.UTC().Format(time.RFC3339Nano)}, nil
}

func (s *Service) KickChatMember(ctx context.Context, user []byte, roomID uuid.UUID, target []byte) (map[string]any, error) {
	now := s.clock.Now()
	if err := s.store.KickMember(ctx, user, roomID, target, now); err != nil {
		return nil, err
	}
	_ = s.appendEvent(ctx, target, "room.memberKicked", map[string]any{"roomId": roomID.String()})
	return map[string]any{"kickedAt": now.UTC().Format(time.RFC3339Nano)}, nil
}

func (s *Service) ListChatMembers(ctx context.Context, roomID uuid.UUID, limit int, cursor string) (map[string]any, error) {
	limit = normalizeLimit(limit, 100)
	offset, err := decodeOffsetCursor(cursor)
	if err != nil {
		return nil, err
	}
	members, err := s.store.ListMembers(ctx, roomID, limit+1, offset)
	if err != nil {
		return nil, err
	}
	members, nextCursor := paginateSlice(members, limit, offset)
	items := make([]map[string]any, 0, len(members))
	for _, member := range members {
		items = append(items, map[string]any{"userPublicKey": member.UserPublicKey, "role": int(member.Role), "joinedAt": member.JoinedAt.UTC().Format(time.RFC3339Nano)})
	}
	return map[string]any{"items": items, "nextCursor": nextCursor, "totalCount": nil}, nil
}

func (s *Service) UpdateChatMemberRole(ctx context.Context, user []byte, roomID uuid.UUID, target []byte, role int16) (map[string]any, error) {
	now := s.clock.Now()
	if err := s.store.UpdateMemberRole(ctx, user, roomID, target, role, now); err != nil {
		return nil, err
	}
	_ = s.appendEvent(ctx, target, "room.memberRoleUpdated", map[string]any{"roomId": roomID.String(), "role": int(role)})
	return map[string]any{"updatedAt": now.UTC().Format(time.RFC3339Nano)}, nil
}

func (s *Service) CreateChatMemberPermission(ctx context.Context, user []byte, roomID uuid.UUID, target []byte, permissionKey string, isAllowed bool) (map[string]any, error) {
	now := s.clock.Now()
	record := ChatMemberPermissionRecord{PermissionID: s.uuidGen.New(), RoomID: roomID, UserPublicKey: target, PermissionKey: permissionKey, IsAllowed: isAllowed, CreatedAt: now, UpdatedAt: now}
	if err := s.store.CreateMemberPermission(ctx, user, record); err != nil {
		return nil, err
	}
	return map[string]any{"id": record.PermissionID, "createdAt": now.UTC().Format(time.RFC3339Nano)}, nil
}

func (s *Service) ListChatMemberPermissions(ctx context.Context, roomID uuid.UUID, target []byte, limit int, cursor string) (map[string]any, error) {
	limit = normalizeLimit(limit, 100)
	offset, err := decodeOffsetCursor(cursor)
	if err != nil {
		return nil, err
	}
	records, err := s.store.ListMemberPermissions(ctx, roomID, target, limit+1, offset)
	if err != nil {
		return nil, err
	}
	records, nextCursor := paginateSlice(records, limit, offset)
	items := make([]map[string]any, 0, len(records))
	for _, record := range records {
		items = append(items, map[string]any{"id": record.PermissionID, "roomId": record.RoomID, "userPublicKey": record.UserPublicKey, "permissionKey": record.PermissionKey, "isAllowed": record.IsAllowed, "createdAt": record.CreatedAt.UTC().Format(time.RFC3339Nano)})
	}
	return map[string]any{"items": items, "nextCursor": nextCursor}, nil
}

func (s *Service) UpdateChatMemberPermission(ctx context.Context, user []byte, permissionID uuid.UUID, isAllowed bool) (map[string]any, error) {
	now := s.clock.Now()
	if err := s.store.UpdateMemberPermission(ctx, user, permissionID, isAllowed, now); err != nil {
		return nil, err
	}
	return map[string]any{"updatedAt": now.UTC().Format(time.RFC3339Nano)}, nil
}

func (s *Service) DeleteChatMemberPermission(ctx context.Context, user []byte, permissionID uuid.UUID) (map[string]any, error) {
	now := s.clock.Now()
	if err := s.store.DeleteMemberPermission(ctx, user, permissionID, now); err != nil {
		return nil, err
	}
	return map[string]any{"deletedAt": now.UTC().Format(time.RFC3339Nano)}, nil
}

func (s *Service) SendChatInvitation(ctx context.Context, user []byte, roomID uuid.UUID, invitee []byte) (map[string]any, error) {
	now := s.clock.Now()
	record := ChatInvitationRecord{InvitationID: s.uuidGen.New(), RoomID: roomID, InviterPublicKey: user, InviteePublicKey: invitee, State: InvitationPending, CreatedAt: now, UpdatedAt: now}
	if err := s.store.CreateInvitation(ctx, record); err != nil {
		return nil, err
	}
	_ = s.appendEvent(ctx, invitee, "room.invitationReceived", map[string]any{"invitationId": record.InvitationID.String(), "roomId": roomID.String()})
	return map[string]any{"invitationId": record.InvitationID, "createdAt": now.UTC().Format(time.RFC3339Nano)}, nil
}

func (s *Service) RevokeChatInvitation(ctx context.Context, user []byte, invitationID uuid.UUID) (map[string]any, error) {
	now := s.clock.Now()
	record, err := s.store.UpdateInvitationState(ctx, invitationID, user, InvitationRevoked, now, []int16{InvitationPending})
	if err != nil {
		return nil, err
	}
	_ = s.appendEvent(ctx, record.InviteePublicKey, "room.invitationRevoked", map[string]any{"invitationId": invitationID.String()})
	return map[string]any{"revokedAt": now.UTC().Format(time.RFC3339Nano)}, nil
}

func (s *Service) ListSentChatInvitations(ctx context.Context, user []byte, roomID *uuid.UUID, limit int, cursor string) (map[string]any, error) {
	limit = normalizeLimit(limit, 50)
	offset, err := decodeOffsetCursor(cursor)
	if err != nil {
		return nil, err
	}
	records, err := s.store.ListSentInvitations(ctx, user, roomID, limit+1, offset)
	if err != nil {
		return nil, err
	}
	records, nextCursor := paginateSlice(records, limit, offset)
	return map[string]any{"items": invitationRecordsToMaps(records), "nextCursor": nextCursor}, nil
}

func (s *Service) ListIncomingChatInvitations(ctx context.Context, user []byte, limit int, cursor string) (map[string]any, error) {
	limit = normalizeLimit(limit, 50)
	offset, err := decodeOffsetCursor(cursor)
	if err != nil {
		return nil, err
	}
	records, err := s.store.ListIncomingInvitations(ctx, user, limit+1, offset)
	if err != nil {
		return nil, err
	}
	records, nextCursor := paginateSlice(records, limit, offset)
	return map[string]any{"items": invitationRecordsToMaps(records), "nextCursor": nextCursor}, nil
}

func (s *Service) AcceptChatInvitation(ctx context.Context, user []byte, invitationID uuid.UUID) (map[string]any, error) {
	now := s.clock.Now()
	var record ChatInvitationRecord
	if err := s.txManager.WithinTransaction(ctx, func(txCtx context.Context) error {
		var txErr error
		record, txErr = s.store.UpdateInvitationState(txCtx, invitationID, user, InvitationAccepted, now, []int16{InvitationPending})
		if txErr != nil {
			return txErr
		}
		if err := s.store.JoinRoom(txCtx, ChatMemberRecord{RoomID: record.RoomID, UserPublicKey: user, Role: RoleMember, NotificationLevel: NotificationAll, JoinedAt: now}); err != nil {
			return err
		}
		return s.appendEvent(txCtx, record.InviterPublicKey, "room.invitationAccepted", map[string]any{"invitationId": invitationID.String(), "roomId": record.RoomID.String()})
	}); err != nil {
		return nil, err
	}
	return map[string]any{"roomId": record.RoomID, "acceptedAt": now.UTC().Format(time.RFC3339Nano)}, nil
}

func (s *Service) DeclineChatInvitation(ctx context.Context, user []byte, invitationID uuid.UUID) (map[string]any, error) {
	now := s.clock.Now()
	record, err := s.store.UpdateInvitationState(ctx, invitationID, user, InvitationDeclined, now, []int16{InvitationPending})
	if err != nil {
		return nil, err
	}
	_ = s.appendEvent(ctx, record.InviterPublicKey, "room.invitationDeclined", map[string]any{"invitationId": invitationID.String()})
	return map[string]any{"declinedAt": now.UTC().Format(time.RFC3339Nano)}, nil
}

func (s *Service) SendMessage(ctx context.Context, user []byte, roomID uuid.UUID, clientMsgID uuid.UUID, body [][]byte) (map[string]any, error) {
	now := s.clock.Now()
	message := MessageRecord{MessageID: s.uuidGen.New(), RoomID: roomID, SenderPublicKey: user, ClientMsgID: clientMsgID, Body: body, CreatedAt: now}
	if err := s.store.CreateMessage(ctx, message); err != nil {
		return nil, err
	}
	members, _ := s.store.ListActiveRoomMemberPublicKeys(ctx, roomID)
	for _, member := range members {
		if string(member) == string(user) {
			continue
		}
		_ = s.appendEvent(ctx, member, "message.created", map[string]any{"roomId": roomID.String(), "messageId": message.MessageID.String(), "senderPublicKey": user})
	}
	return map[string]any{"messageId": message.MessageID, "createdAt": now.UTC().Format(time.RFC3339Nano)}, nil
}

func (s *Service) DeleteMessage(ctx context.Context, user []byte, roomID uuid.UUID, messageID uuid.UUID) (map[string]any, error) {
	now := s.clock.Now()
	if err := s.store.DeleteMessage(ctx, user, roomID, messageID, now); err != nil {
		return nil, err
	}
	members, _ := s.store.ListActiveRoomMemberPublicKeys(ctx, roomID)
	for _, member := range members {
		_ = s.appendEvent(ctx, member, "message.deleted", map[string]any{"roomId": roomID.String(), "messageId": messageID.String()})
	}
	return map[string]any{"deletedAt": now.UTC().Format(time.RFC3339Nano)}, nil
}

func (s *Service) GetServerLimits(ctx context.Context) (map[string]any, error) {
	stats, err := s.store.CountServerStats(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{"limits": map[string]any{"eventBatchSize": s.cfg.EventBatchSize, "challengeTTLSeconds": int64(s.cfg.SessionChallengeTTL.Seconds())}, "spent": map[string]any{"profiles": stats.Profiles, "devices": stats.Devices, "friends": stats.Friends, "rooms": stats.Rooms, "messages": stats.Messages}}, nil
}

func (s *Service) GetUserLimits(ctx context.Context, user []byte) (map[string]any, error) {
	stats, err := s.store.CountUserStats(ctx, user)
	if err != nil {
		return nil, err
	}
	return map[string]any{"limits": map[string]any{"devices": 10, "keyPackages": 1000, "rooms": 100}, "spent": map[string]any{"devices": stats.Devices, "keyPackages": stats.KeyPackages, "friends": stats.Friends, "outgoingFriendRequests": stats.OutgoingFriendRequests, "rooms": stats.Rooms}}, nil
}

func (s *Service) GetGroupLimits(ctx context.Context, roomID uuid.UUID) (map[string]any, error) {
	stats, err := s.store.CountGroupStats(ctx, roomID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"limits": map[string]any{"members": 1000, "messages": 1000000, "invites": 10000}, "spent": map[string]any{"members": stats.Members, "messages": stats.Messages, "invites": stats.Invites}}, nil
}

func (s *Service) GetServerConfig() map[string]any {
	return map[string]any{"config": map[string]any{"updatedAt": s.clock.Now().UTC().Format(time.RFC3339Nano), "version": s.cfg.Version}}
}

func (s *Service) appendEvent(ctx context.Context, user []byte, eventType string, payload map[string]any) error {
	if len(user) != ed25519.PublicKeySize {
		return nil
	}
	now := s.clock.Now()
	return s.events.Append(ctx, domainauth.Event{EventID: s.uuidGen.New(), UserPublicKey: append([]byte(nil), user...), EventType: eventType, Payload: payload, AvailableAt: now, ExpiresAt: now.Add(s.cfg.EventRetention), CreatedAt: now})
}

var (
	ErrNotFound        = errors.New("not found")
	ErrForbidden       = errors.New("forbidden")
	ErrConflict        = errors.New("conflict")
	ErrProfileRequired = errors.New("profile must be completed before using this RPC")
)
