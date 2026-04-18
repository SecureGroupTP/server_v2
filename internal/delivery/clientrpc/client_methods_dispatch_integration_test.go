package clientrpc

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	clientapi "server_v2/internal/application/clientapi"
	domainauth "server_v2/internal/domain/auth"
)

type dispatchClock struct{ now time.Time }

func (c dispatchClock) Now() time.Time { return c.now }

type dispatchUUIDs struct{ ids []uuid.UUID }

func (g *dispatchUUIDs) New() uuid.UUID {
	id := g.ids[0]
	g.ids = g.ids[1:]
	return id
}

type dispatchTx struct{}

func (dispatchTx) WithinTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

type dispatchEvents struct{}

func (dispatchEvents) Append(context.Context, domainauth.Event) error { return nil }

type dispatchSessions struct{}

func (dispatchSessions) LookupSession(context.Context, uuid.UUID) (domainauth.Session, error) {
	return domainauth.Session{DeviceID: "device-1"}, nil
}

type dispatchStore struct {
	now         time.Time
	roomID      uuid.UUID
	groupID     uuid.UUID
	invitation  clientapi.ChatInvitationRecord
	directRoom  clientapi.DirectRoomRecord
	roomMembers [][]byte
}

func (s *dispatchStore) GetProfile(context.Context, []byte) (clientapi.ProfileRecord, error) {
	return clientapi.ProfileRecord{PublicKey: key(9), Username: "@u", LastSeenAt: s.now}, nil
}
func (s *dispatchStore) GetActiveBanStatus(context.Context, []byte, time.Time) (clientapi.BanStatusRecord, bool, error) {
	return clientapi.BanStatusRecord{}, false, nil
}
func (s *dispatchStore) UpdateProfile(context.Context, []byte, string, string, string, string, time.Time) error {
	return nil
}
func (s *dispatchStore) SearchProfiles(context.Context, string, int, int) ([]clientapi.ProfileRecord, error) {
	return []clientapi.ProfileRecord{{PublicKey: key(2), Username: "@hit", LastSeenAt: s.now}}, nil
}
func (s *dispatchStore) DeleteAccount(context.Context, []byte, time.Time) error { return nil }
func (s *dispatchStore) GetProfileAvatar(context.Context, []byte) (clientapi.AvatarRecord, error) {
	return clientapi.AvatarRecord{Bytes: []byte("avatar"), ContentType: "image/png"}, nil
}
func (s *dispatchStore) ListDevices(context.Context, []byte) ([]clientapi.DeviceRecord, error) {
	return []clientapi.DeviceRecord{{ID: uuid.New(), Platform: 1, PushToken: "push", IsEnabled: true, UpdatedAt: s.now}}, nil
}
func (s *dispatchStore) UpsertDevice(context.Context, clientapi.DeviceRecord) (clientapi.DeviceRecord, error) {
	return clientapi.DeviceRecord{ID: uuid.New(), UpdatedAt: s.now}, nil
}
func (s *dispatchStore) RemoveDevice(context.Context, []byte, uuid.UUID, time.Time) error { return nil }
func (s *dispatchStore) InsertKeyPackages(_ context.Context, packages []clientapi.KeyPackageRecord) (int, error) {
	return len(packages), nil
}
func (s *dispatchStore) FetchKeyPackages(context.Context, [][]byte, time.Time) ([]clientapi.KeyPackageRecord, error) {
	return []clientapi.KeyPackageRecord{{UserPublicKey: key(2), DeviceID: "device-2", KeyPackageBytes: []byte("kp"), ExpiresAt: s.now.Add(time.Hour)}}, nil
}
func (s *dispatchStore) DeleteKeyPackagesByUserDevice(context.Context, []byte, string) error {
	return nil
}
func (s *dispatchStore) UpsertRoomGroupInfo(context.Context, []byte, clientapi.ChatRoomGroupInfoRecord) error {
	return nil
}
func (s *dispatchStore) GetRoomGroupInfo(context.Context, uuid.UUID) (clientapi.ChatRoomGroupInfoRecord, error) {
	return clientapi.ChatRoomGroupInfoRecord{RoomID: s.roomID, GroupInfoBytes: []byte("group-info")}, nil
}
func (s *dispatchStore) FindDirectRoomIDByUsers(context.Context, []byte, []byte) (uuid.UUID, bool, error) {
	if s.directRoom.RoomID == uuid.Nil {
		return uuid.Nil, false, nil
	}
	return s.directRoom.RoomID, true, nil
}
func (s *dispatchStore) CreateDirectRoom(context.Context, clientapi.ChatRoomRecord, clientapi.ChatMemberRecord, clientapi.ChatMemberRecord, clientapi.DirectRoomRecord) error {
	return nil
}
func (s *dispatchStore) IsDirectRoom(context.Context, uuid.UUID) (bool, error) { return true, nil }
func (s *dispatchStore) UpsertRoomWelcome(context.Context, []byte, clientapi.ChatRoomWelcomeRecord) error {
	return nil
}
func (s *dispatchStore) GetRoomWelcome(context.Context, uuid.UUID, []byte, string) (clientapi.ChatRoomWelcomeRecord, error) {
	return clientapi.ChatRoomWelcomeRecord{WelcomeBytes: []byte("welcome")}, nil
}
func (s *dispatchStore) DeleteRoomWelcomesByTargetUser(context.Context, []byte) error { return nil }
func (s *dispatchStore) AreFriends(context.Context, []byte, []byte) (bool, error)     { return true, nil }
func (s *dispatchStore) ListFriends(context.Context, []byte, int, int) ([]clientapi.FriendRecord, error) {
	return []clientapi.FriendRecord{{ID: uuid.New(), UserPublicKey: key(1), FriendPublicKey: key(2), AcceptedAt: s.now}}, nil
}
func (s *dispatchStore) CountFriends(context.Context, []byte) (int64, error) { return 1, nil }
func (s *dispatchStore) RemoveFriend(context.Context, []byte, []byte, time.Time) error {
	return nil
}
func (s *dispatchStore) CreateFriendRequest(context.Context, clientapi.FriendRequestRecord) error {
	return nil
}
func (s *dispatchStore) UpdateFriendRequestState(context.Context, uuid.UUID, []byte, []int16, int16, time.Time) (clientapi.FriendRequestRecord, error) {
	return clientapi.FriendRequestRecord{RequestID: uuid.New(), SenderPublicKey: key(1), ReceiverPublicKey: key(2), State: clientapi.FriendRequestAccepted}, nil
}
func (s *dispatchStore) GetFriendRequest(context.Context, uuid.UUID) (clientapi.FriendRequestRecord, error) {
	return clientapi.FriendRequestRecord{}, nil
}
func (s *dispatchStore) ListFriendRequests(context.Context, []byte, string, int, int) ([]clientapi.FriendRequestRecord, error) {
	return []clientapi.FriendRequestRecord{{RequestID: uuid.New(), SenderPublicKey: key(1), ReceiverPublicKey: key(2), State: clientapi.FriendRequestPending}}, nil
}
func (s *dispatchStore) CreateFriendPair(context.Context, clientapi.FriendRecord, clientapi.FriendRecord) error {
	return nil
}
func (s *dispatchStore) CreateRoom(context.Context, clientapi.ChatRoomRecord, clientapi.ChatMemberRecord) error {
	return nil
}
func (s *dispatchStore) ListRooms(context.Context, []byte, int, int) ([]clientapi.ChatRoomRecord, error) {
	return []clientapi.ChatRoomRecord{{RoomID: s.roomID, Title: "Room", Visibility: clientapi.VisibilityPublic, UpdatedAt: s.now}}, nil
}
func (s *dispatchStore) GetRoom(context.Context, uuid.UUID) (clientapi.ChatRoomRecord, error) {
	return clientapi.ChatRoomRecord{RoomID: s.roomID, OwnerPublicKey: key(1), Title: "Room", Visibility: clientapi.VisibilityPublic, UpdatedAt: s.now}, nil
}
func (s *dispatchStore) SearchRooms(context.Context, string, int, int) ([]clientapi.ChatRoomRecord, error) {
	return []clientapi.ChatRoomRecord{{RoomID: s.roomID, Title: "Room", UpdatedAt: s.now}}, nil
}
func (s *dispatchStore) UpdateRoom(context.Context, []byte, uuid.UUID, string, string, string, time.Time) error {
	return nil
}
func (s *dispatchStore) DeleteRoom(context.Context, []byte, uuid.UUID, time.Time) error { return nil }
func (s *dispatchStore) GetRoomAvatar(context.Context, uuid.UUID) (clientapi.AvatarRecord, error) {
	return clientapi.AvatarRecord{Bytes: []byte("room-avatar"), ContentType: "image/png"}, nil
}
func (s *dispatchStore) AddRoomState(context.Context, []byte, clientapi.ChatRoomStateRecord) error {
	return nil
}
func (s *dispatchStore) FetchRoomState(context.Context, []byte, uuid.UUID, int64) (clientapi.ChatRoomStateRecord, error) {
	return clientapi.ChatRoomStateRecord{RoomID: s.roomID, GroupID: s.groupID, Epoch: 1, TreeBytes: []byte("tree"), TreeHash: []byte("hash")}, nil
}
func (s *dispatchStore) JoinRoom(context.Context, clientapi.ChatMemberRecord) error { return nil }
func (s *dispatchStore) UpsertRoomMembership(context.Context, clientapi.ChatMemberRecord) error {
	return nil
}
func (s *dispatchStore) LeaveRoom(context.Context, uuid.UUID, []byte, time.Time) error { return nil }
func (s *dispatchStore) KickMember(context.Context, []byte, uuid.UUID, []byte, time.Time) error {
	return nil
}
func (s *dispatchStore) ListMembers(context.Context, uuid.UUID, int, int) ([]clientapi.ChatMemberRecord, error) {
	return []clientapi.ChatMemberRecord{{RoomID: s.roomID, UserPublicKey: key(2), Role: clientapi.RoleMember, JoinedAt: s.now}}, nil
}
func (s *dispatchStore) UpdateMemberRole(context.Context, []byte, uuid.UUID, []byte, int16, time.Time) error {
	return nil
}
func (s *dispatchStore) CreateMemberPermission(context.Context, []byte, clientapi.ChatMemberPermissionRecord) error {
	return nil
}
func (s *dispatchStore) ListMemberPermissions(context.Context, uuid.UUID, []byte, int, int) ([]clientapi.ChatMemberPermissionRecord, error) {
	return []clientapi.ChatMemberPermissionRecord{{PermissionID: uuid.New(), RoomID: s.roomID, UserPublicKey: key(2), PermissionKey: "send", IsAllowed: true, CreatedAt: s.now}}, nil
}
func (s *dispatchStore) UpdateMemberPermission(context.Context, []byte, uuid.UUID, bool, time.Time) error {
	return nil
}
func (s *dispatchStore) DeleteMemberPermission(context.Context, []byte, uuid.UUID, time.Time) error {
	return nil
}
func (s *dispatchStore) CreateInvitation(context.Context, clientapi.ChatInvitationRecord) error {
	return nil
}
func (s *dispatchStore) GetInvitation(context.Context, uuid.UUID) (clientapi.ChatInvitationRecord, error) {
	return s.invitation, nil
}
func (s *dispatchStore) ListSentInvitations(context.Context, []byte, *uuid.UUID, int, int) ([]clientapi.ChatInvitationRecord, error) {
	return []clientapi.ChatInvitationRecord{s.invitation}, nil
}
func (s *dispatchStore) ListIncomingInvitations(context.Context, []byte, int, int) ([]clientapi.ChatInvitationRecord, error) {
	return []clientapi.ChatInvitationRecord{s.invitation}, nil
}
func (s *dispatchStore) UpdateInvitationState(context.Context, uuid.UUID, []byte, int16, time.Time, []int16) (clientapi.ChatInvitationRecord, error) {
	return s.invitation, nil
}
func (s *dispatchStore) FindPendingInvitation(context.Context, uuid.UUID, []byte) (clientapi.ChatInvitationRecord, bool, error) {
	return s.invitation, true, nil
}
func (s *dispatchStore) ListActiveRoomMemberPublicKeys(context.Context, uuid.UUID) ([][]byte, error) {
	return s.roomMembers, nil
}
func (s *dispatchStore) CountServerStats(context.Context) (clientapi.ServerStats, error) {
	return clientapi.ServerStats{Profiles: 1}, nil
}
func (s *dispatchStore) CountUserStats(context.Context, []byte) (clientapi.UserStats, error) {
	return clientapi.UserStats{Devices: 1}, nil
}
func (s *dispatchStore) CountGroupStats(context.Context, uuid.UUID) (clientapi.GroupStats, error) {
	return clientapi.GroupStats{Members: 2}, nil
}

func (s *dispatchStore) RecordUserUsage(context.Context, []byte, time.Time, int64, int64, int64) error {
	return nil
}

func (s *dispatchStore) GetUserUsageStats(context.Context, []byte, time.Time) (clientapi.UsageStats, error) {
	return clientapi.UsageStats{}, nil
}

func TestDispatchClientMethodRoutesAllGroups(t *testing.T) {
	now := time.Date(2026, 4, 12, 16, 0, 0, 0, time.UTC)
	roomID := uuid.MustParse("aaaaaaaa-1111-2222-3333-bbbbbbbbbbbb")
	groupID := uuid.MustParse("bbbbbbbb-1111-2222-3333-cccccccccccc")
	invitationID := uuid.MustParse("cccccccc-1111-2222-3333-dddddddddddd")
	store := &dispatchStore{
		now:         now,
		roomID:      roomID,
		groupID:     groupID,
		invitation:  clientapi.ChatInvitationRecord{InvitationID: invitationID, RoomID: roomID, InviterPublicKey: key(1), InviteePublicKey: key(2), State: clientapi.InvitationPending, CreatedAt: now, UpdatedAt: now},
		directRoom:  clientapi.DirectRoomRecord{RoomID: roomID, LeftUserPublicKey: key(1), RightUserPublicKey: key(2), CreatedAt: now},
		roomMembers: [][]byte{key(1), key(2)},
	}
	ids := make([]uuid.UUID, 0, 120)
	for i := 0; i < 120; i++ {
		ids = append(ids, uuid.New())
	}
	service, err := clientapi.NewService(clientapi.Config{Version: "2", EventRetention: time.Hour, EventBatchSize: 10}, dispatchClock{now: now}, &dispatchUUIDs{ids: ids}, dispatchTx{}, store, dispatchEvents{}, dispatchSessions{})
	if err != nil {
		t.Fatalf("new client service: %v", err)
	}
	handler := NewHandler(slog.Default(), nil, service, nil, nil)
	state := sessionState{SessionID: uuid.New(), UserPublicKey: key(1), Authenticated: true, ProfileCompleted: true}
	params := map[string]any{
		"userPublicKey":        key(2),
		"friendPublicKey":      key(2),
		"receiverPublicKey":    key(2),
		"targetUserPublicKey":  key(2),
		"inviteePublicKey":     key(2),
		"roomId":               roomID.String(),
		"groupId":              groupID.String(),
		"deviceId":             uuid.New().String(),
		"requestId":            uuid.New().String(),
		"invitationId":         invitationID.String(),
		"permissionId":         uuid.New().String(),
		"clientMsgId":          uuid.New().String(),
		"title":                "Room",
		"description":          "Desc",
		"avatarHash":           "hash",
		"query":                "room",
		"visibility":           uint64(clientapi.VisibilityPublic),
		"platform":             uint64(1),
		"pushToken":            "push",
		"isEnabled":            true,
		"epoch":                uint64(1),
		"role":                 uint64(clientapi.RoleAdmin),
		"permissionKey":        "send",
		"isAllowed":            true,
		"limit":                uint64(5),
		"cursor":               "",
		"direction":            "incoming",
		"commitBytes":          []byte("commit"),
		"welcomeBytes":         []byte("welcome"),
		"groupInfoBytes":       []byte("group-info"),
		"treeBytes":            []byte("tree"),
		"treeHash":             []byte("hash"),
		"body":                 []any{[]byte("cipher")},
		"userPublicKeys":       []any{key(2)},
		"inviteToken":          []byte("token"),
		"inviteTokenSignature": []byte("sig"),
		"expiresAt":            now.Format(time.RFC3339Nano),
		"packages":             []any{map[string]any{"keyPackageBytes": []byte("kp"), "expiresAt": now.Add(time.Hour), "isLastResort": true}},
	}
	methods := []string{
		"getProfile", "updateProfile", "searchProfiles", "deleteAccount", "getProfileAvatar",
		"listDevices", "registerDevicePushToken", "removeDevice", "uploadKeyPackages", "fetchKeyPackages", "sendCommit", "sendWelcome", "uploadGroupInfo", "fetchGroupInfo", "sendExternalCommit", "fetchWelcome",
		"listFriends", "removeFriend", "sendFriendRequest", "acceptFriendRequest", "declineFriendRequest", "cancelFriendRequest", "listFriendRequests",
		"createChatRoom", "createDirectRoom", "listChatRooms", "getChatRoom", "searchChatRooms", "syncChatRoom", "updateChatRoom", "updateChatRoomState", "fetchChatRoomState", "deleteChatRoom", "getChatRoomAvatar",
		"joinChatRoom", "leaveChatRoom", "kickChatMember", "listChatMembers", "updateChatMemberRole", "createChatMemberPermission", "listChatMemberPermissions", "updateChatMemberPermission", "deleteChatMemberPermission",
		"sendChatInvitation", "revokeChatInvitation", "listSentChatInvitations", "listIncomingChatInvitations", "acceptChatInvitation", "declineChatInvitation",
		"sendMessage",
		"getServerLimits", "getUserLimits", "getGroupLimits", "getServerConfig",
	}
	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			response, err := handler.dispatchClientMethod(context.Background(), method, params, state)
			if err != nil {
				t.Fatalf("dispatch %s: %v", method, err)
			}
			if response == nil {
				t.Fatalf("dispatch %s returned nil response", method)
			}
		})
	}
	if response, err := handler.dispatchClientMethod(context.Background(), "unknownMethod", params, state); err == nil || response != nil {
		t.Fatalf("unknown dispatch response=%#v err=%v", response, err)
	}
}

func key(value byte) []byte {
	out := make([]byte, 32)
	for i := range out {
		out[i] = value
	}
	return out
}
