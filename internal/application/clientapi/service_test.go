package clientapi

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	domainauth "server_v2/internal/domain/auth"
)

type fixedClock struct{ now time.Time }

func (f fixedClock) Now() time.Time { return f.now }

type noopTxManager struct{}

func (noopTxManager) WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	return fn(ctx)
}

type sequenceUUID struct{ ids []uuid.UUID }

func (s *sequenceUUID) New() uuid.UUID { id := s.ids[0]; s.ids = s.ids[1:]; return id }

type storeMock struct {
	profileUpdated bool
	friendRequest  FriendRequestRecord
	roomCreated    ChatRoomRecord
	memberCreated  ChatMemberRecord
	banStatus      BanStatusRecord
	hasBanStatus   bool
	profiles       []ProfileRecord
	friends        []FriendRecord
	friendCount    int64
}

func (s *storeMock) GetProfile(context.Context, []byte) (ProfileRecord, error) {
	return ProfileRecord{}, nil
}

func (s *storeMock) GetActiveBanStatus(context.Context, []byte, time.Time) (BanStatusRecord, bool, error) {
	return s.banStatus, s.hasBanStatus, nil
}

func (s *storeMock) UpdateProfile(_ context.Context, _ []byte, _, _, _, _ string, _ time.Time) error {
	s.profileUpdated = true
	return nil
}

func (s *storeMock) SearchProfiles(context.Context, string, int, int) ([]ProfileRecord, error) {
	return s.profiles, nil
}
func (s *storeMock) DeleteAccount(context.Context, []byte, time.Time) error { return nil }
func (s *storeMock) GetProfileAvatar(context.Context, []byte) (AvatarRecord, error) {
	return AvatarRecord{}, nil
}
func (s *storeMock) ListDevices(context.Context, []byte) ([]DeviceRecord, error) { return nil, nil }
func (s *storeMock) UpsertDevice(context.Context, DeviceRecord) (DeviceRecord, error) {
	return DeviceRecord{}, nil
}
func (s *storeMock) RemoveDevice(context.Context, []byte, uuid.UUID, time.Time) error { return nil }
func (s *storeMock) InsertKeyPackages(context.Context, []KeyPackageRecord) (int, error) {
	return 0, nil
}

func (s *storeMock) FetchKeyPackages(context.Context, [][]byte, time.Time) ([]KeyPackageRecord, error) {
	return nil, nil
}
func (s *storeMock) DeleteKeyPackagesByUserDevice(context.Context, []byte, string) error { return nil }
func (s *storeMock) ListFriends(context.Context, []byte, int, int) ([]FriendRecord, error) {
	return s.friends, nil
}
func (s *storeMock) CountFriends(context.Context, []byte) (int64, error)           { return s.friendCount, nil }
func (s *storeMock) RemoveFriend(context.Context, []byte, []byte, time.Time) error { return nil }
func (s *storeMock) CreateFriendRequest(_ context.Context, request FriendRequestRecord) error {
	s.friendRequest = request
	return nil
}

func (s *storeMock) UpdateFriendRequestState(context.Context, uuid.UUID, []byte, []int16, int16, time.Time) (FriendRequestRecord, error) {
	return FriendRequestRecord{}, nil
}

func (s *storeMock) GetFriendRequest(context.Context, uuid.UUID) (FriendRequestRecord, error) {
	return FriendRequestRecord{}, nil
}

func (s *storeMock) ListFriendRequests(context.Context, []byte, string, int, int) ([]FriendRequestRecord, error) {
	return nil, nil
}
func (s *storeMock) CreateFriendPair(context.Context, FriendRecord, FriendRecord) error { return nil }
func (s *storeMock) CreateRoom(_ context.Context, room ChatRoomRecord, owner ChatMemberRecord) error {
	s.roomCreated = room
	s.memberCreated = owner
	return nil
}

func (s *storeMock) ListRooms(context.Context, []byte, int, int) ([]ChatRoomRecord, error) {
	return nil, nil
}

func (s *storeMock) GetRoom(context.Context, uuid.UUID) (ChatRoomRecord, error) {
	return ChatRoomRecord{}, nil
}

func (s *storeMock) SearchRooms(context.Context, string, int, int) ([]ChatRoomRecord, error) {
	return nil, nil
}

func (s *storeMock) UpdateRoom(context.Context, []byte, uuid.UUID, string, string, string, time.Time) error {
	return nil
}
func (s *storeMock) DeleteRoom(context.Context, []byte, uuid.UUID, time.Time) error { return nil }
func (s *storeMock) GetRoomAvatar(context.Context, uuid.UUID) (AvatarRecord, error) {
	return AvatarRecord{}, nil
}
func (s *storeMock) AddRoomState(context.Context, []byte, ChatRoomStateRecord) error { return nil }
func (s *storeMock) FetchRoomState(context.Context, []byte, uuid.UUID, int64) (ChatRoomStateRecord, error) {
	return ChatRoomStateRecord{}, nil
}
func (s *storeMock) JoinRoom(context.Context, ChatMemberRecord) error              { return nil }
func (s *storeMock) LeaveRoom(context.Context, uuid.UUID, []byte, time.Time) error { return nil }
func (s *storeMock) KickMember(context.Context, []byte, uuid.UUID, []byte, time.Time) error {
	return nil
}

func (s *storeMock) ListMembers(context.Context, uuid.UUID, int, int) ([]ChatMemberRecord, error) {
	return nil, nil
}

func (s *storeMock) UpdateMemberRole(context.Context, []byte, uuid.UUID, []byte, int16, time.Time) error {
	return nil
}

func (s *storeMock) CreateMemberPermission(context.Context, []byte, ChatMemberPermissionRecord) error {
	return nil
}

func (s *storeMock) ListMemberPermissions(context.Context, uuid.UUID, []byte, int, int) ([]ChatMemberPermissionRecord, error) {
	return nil, nil
}

func (s *storeMock) UpdateMemberPermission(context.Context, []byte, uuid.UUID, bool, time.Time) error {
	return nil
}

func (s *storeMock) DeleteMemberPermission(context.Context, []byte, uuid.UUID, time.Time) error {
	return nil
}
func (s *storeMock) CreateInvitation(context.Context, ChatInvitationRecord) error { return nil }
func (s *storeMock) ListSentInvitations(context.Context, []byte, *uuid.UUID, int, int) ([]ChatInvitationRecord, error) {
	return nil, nil
}

func (s *storeMock) ListIncomingInvitations(context.Context, []byte, int, int) ([]ChatInvitationRecord, error) {
	return nil, nil
}

func (s *storeMock) UpdateInvitationState(context.Context, uuid.UUID, []byte, int16, time.Time, []int16) (ChatInvitationRecord, error) {
	return ChatInvitationRecord{}, nil
}
func (s *storeMock) CreateMessage(context.Context, MessageRecord) error { return nil }
func (s *storeMock) DeleteMessage(context.Context, []byte, uuid.UUID, uuid.UUID, time.Time) error {
	return nil
}

func (s *storeMock) ListActiveRoomMemberPublicKeys(context.Context, uuid.UUID) ([][]byte, error) {
	return nil, nil
}
func (s *storeMock) CountServerStats(context.Context) (ServerStats, error) { return ServerStats{}, nil }
func (s *storeMock) CountUserStats(context.Context, []byte) (UserStats, error) {
	return UserStats{}, nil
}

func (s *storeMock) CountGroupStats(context.Context, uuid.UUID) (GroupStats, error) {
	return GroupStats{}, nil
}

type eventAppenderMock struct{ events []domainauth.Event }

func (e *eventAppenderMock) Append(_ context.Context, event domainauth.Event) error {
	e.events = append(e.events, event)
	return nil
}

type sessionLookupMock struct{}

func (sessionLookupMock) LookupSession(context.Context, uuid.UUID) (domainauth.Session, error) {
	return domainauth.Session{DeviceID: "device-1"}, nil
}

func TestServiceSendFriendRequestAppendsEvent(t *testing.T) {
	now := time.Date(2026, 4, 11, 15, 0, 0, 0, time.UTC)
	store := &storeMock{}
	events := &eventAppenderMock{}
	service, err := NewService(Config{Version: "2", EventRetention: time.Hour}, fixedClock{now: now}, &sequenceUUID{ids: []uuid.UUID{uuid.MustParse("11111111-1111-1111-1111-111111111111"), uuid.MustParse("22222222-2222-2222-2222-222222222222")}}, noopTxManager{}, store, events, sessionLookupMock{})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, err = service.SendFriendRequest(context.Background(), make([]byte, 32), bytes32(7))
	if err != nil {
		t.Fatalf("send friend request: %v", err)
	}
	if store.friendRequest.RequestID == uuid.Nil {
		t.Fatal("expected request id")
	}
	if len(events.events) != 1 {
		t.Fatalf("expected one event, got %d", len(events.events))
	}
}

func TestServiceUpdateProfileAppendsEventsToFriends(t *testing.T) {
	now := time.Date(2026, 4, 11, 15, 30, 0, 0, time.UTC)
	friendKey := bytes32(9)
	store := &storeMock{friends: []FriendRecord{{FriendPublicKey: friendKey}}}
	events := &eventAppenderMock{}
	service, err := NewService(Config{Version: "2", EventRetention: time.Hour, EventBatchSize: 100}, fixedClock{now: now}, &sequenceUUID{ids: []uuid.UUID{uuid.MustParse("33333333-3333-3333-3333-333333333333")}}, noopTxManager{}, store, events, sessionLookupMock{})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, err = service.UpdateProfile(context.Background(), bytes32(1), "@alice", "Alice Cooper", "", "bio")
	if err != nil {
		t.Fatalf("update profile: %v", err)
	}
	if len(events.events) != 1 {
		t.Fatalf("expected one event, got %d", len(events.events))
	}
	if events.events[0].EventType != "profile.updated" {
		t.Fatalf("unexpected event type: %s", events.events[0].EventType)
	}
	if string(events.events[0].UserPublicKey) != string(friendKey) {
		t.Fatalf("unexpected event receiver: %#v", events.events[0].UserPublicKey)
	}
	if events.events[0].Payload["displayName"] != "Alice Cooper" {
		t.Fatalf("unexpected event payload: %#v", events.events[0].Payload)
	}
	if events.events[0].Payload["username"] != "@alice" {
		t.Fatalf("unexpected event payload: %#v", events.events[0].Payload)
	}
}

func TestServiceCreateChatRoomCreatesOwnerMembership(t *testing.T) {
	now := time.Date(2026, 4, 11, 16, 0, 0, 0, time.UTC)
	roomID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	store := &storeMock{}
	service, err := NewService(Config{Version: "2", EventRetention: time.Hour}, fixedClock{now: now}, &sequenceUUID{ids: []uuid.UUID{roomID}}, noopTxManager{}, store, &eventAppenderMock{}, sessionLookupMock{})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, err = service.CreateChatRoom(context.Background(), bytes32(1), "Room", "Desc", VisibilityPublic)
	if err != nil {
		t.Fatalf("create room: %v", err)
	}
	if store.roomCreated.RoomID != roomID {
		t.Fatalf("unexpected room id: %s", store.roomCreated.RoomID)
	}
	if store.memberCreated.Role != RoleOwner {
		t.Fatalf("expected owner role, got %d", store.memberCreated.Role)
	}
}

func TestServiceGetProfileIncludesBanStatus(t *testing.T) {
	now := time.Date(2026, 4, 11, 18, 0, 0, 0, time.UTC)
	store := &storeMock{
		hasBanStatus: true,
		banStatus: BanStatusRecord{
			IsBanned: true,
			Reason:   "spam",
			BannedAt: now,
		},
	}
	service, err := NewService(Config{Version: "2", EventRetention: time.Hour}, fixedClock{now: now}, &sequenceUUID{ids: []uuid.UUID{uuid.New()}}, noopTxManager{}, store, &eventAppenderMock{}, sessionLookupMock{})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	response, err := service.GetProfile(context.Background(), bytes32(3))
	if err != nil {
		t.Fatalf("get profile: %v", err)
	}
	banStatus, ok := response["banStatus"].(map[string]any)
	if !ok {
		t.Fatalf("expected banStatus map, got %#v", response["banStatus"])
	}
	if banStatus["isBanned"] != true || banStatus["reason"] != "spam" {
		t.Fatalf("unexpected banStatus: %#v", banStatus)
	}
}

func TestServiceSearchProfilesProvidesCursor(t *testing.T) {
	now := time.Date(2026, 4, 11, 19, 0, 0, 0, time.UTC)
	store := &storeMock{
		profiles: []ProfileRecord{
			{PublicKey: bytes32(1), Username: "u1", DisplayName: "One"},
			{PublicKey: bytes32(2), Username: "u2", DisplayName: "Two"},
			{PublicKey: bytes32(3), Username: "u3", DisplayName: "Three"},
		},
	}
	service, err := NewService(Config{Version: "2", EventRetention: time.Hour}, fixedClock{now: now}, &sequenceUUID{ids: []uuid.UUID{uuid.New()}}, noopTxManager{}, store, &eventAppenderMock{}, sessionLookupMock{})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	response, err := service.SearchProfiles(context.Background(), "u", 2, "")
	if err != nil {
		t.Fatalf("search profiles: %v", err)
	}
	items, ok := response["items"].([]map[string]any)
	if !ok || len(items) != 2 {
		t.Fatalf("unexpected items: %#v", response["items"])
	}
	if response["nextCursor"] == nil {
		t.Fatalf("expected nextCursor, got %#v", response)
	}
}

func TestServiceListFriendsIncludesTotalCount(t *testing.T) {
	now := time.Date(2026, 4, 11, 20, 0, 0, 0, time.UTC)
	store := &storeMock{
		friends: []FriendRecord{
			{ID: uuid.MustParse("44444444-4444-4444-4444-444444444444"), FriendPublicKey: bytes32(4), AcceptedAt: now},
			{ID: uuid.MustParse("55555555-5555-5555-5555-555555555555"), FriendPublicKey: bytes32(5), AcceptedAt: now},
		},
		friendCount: 7,
	}
	service, err := NewService(Config{Version: "2", EventRetention: time.Hour}, fixedClock{now: now}, &sequenceUUID{ids: []uuid.UUID{uuid.New()}}, noopTxManager{}, store, &eventAppenderMock{}, sessionLookupMock{})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	response, err := service.ListFriends(context.Background(), bytes32(1), 2, "")
	if err != nil {
		t.Fatalf("list friends: %v", err)
	}
	if response["totalCount"] != int64(7) {
		t.Fatalf("expected totalCount 7, got %#v", response["totalCount"])
	}
}

func bytes32(value byte) []byte {
	out := make([]byte, 32)
	for i := range out {
		out[i] = value
	}
	return out
}
