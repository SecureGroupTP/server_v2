package clientapi

import (
	"context"
	"errors"
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
	invitation     ChatInvitationRecord
	roomCreated    ChatRoomRecord
	memberCreated  ChatMemberRecord
	messageCreated MessageRecord
	groupInfo      ChatRoomGroupInfoRecord
	welcome        ChatRoomWelcomeRecord
	directRoom     DirectRoomRecord
	banStatus      BanStatusRecord
	hasBanStatus   bool
	profiles       []ProfileRecord
	friends        []FriendRecord
	friendCount    int64
	areFriends     bool
	keyPackages    []KeyPackageRecord
	roomMembers    [][]byte
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
	return s.keyPackages, nil
}
func (s *storeMock) DeleteKeyPackagesByUserDevice(context.Context, []byte, string) error { return nil }
func (s *storeMock) UpsertRoomGroupInfo(_ context.Context, _ []byte, groupInfo ChatRoomGroupInfoRecord) error {
	s.groupInfo = groupInfo
	return nil
}
func (s *storeMock) GetRoomGroupInfo(context.Context, uuid.UUID) (ChatRoomGroupInfoRecord, error) {
	return s.groupInfo, nil
}
func (s *storeMock) FindDirectRoomIDByUsers(context.Context, []byte, []byte) (uuid.UUID, bool, error) {
	if s.directRoom.RoomID == uuid.Nil {
		return uuid.Nil, false, nil
	}
	return s.directRoom.RoomID, true, nil
}
func (s *storeMock) CreateDirectRoom(_ context.Context, room ChatRoomRecord, left ChatMemberRecord, right ChatMemberRecord, direct DirectRoomRecord) error {
	s.roomCreated = room
	s.memberCreated = left
	s.roomMembers = [][]byte{left.UserPublicKey, right.UserPublicKey}
	s.directRoom = direct
	return nil
}
func (s *storeMock) IsDirectRoom(context.Context, uuid.UUID) (bool, error) {
	return s.directRoom.RoomID != uuid.Nil, nil
}
func (s *storeMock) UpsertRoomWelcome(_ context.Context, welcome ChatRoomWelcomeRecord) error {
	s.welcome = welcome
	return nil
}
func (s *storeMock) GetRoomWelcome(context.Context, uuid.UUID, []byte) (ChatRoomWelcomeRecord, error) {
	return s.welcome, nil
}
func (s *storeMock) AreFriends(context.Context, []byte, []byte) (bool, error) {
	return s.areFriends, nil
}
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
	return s.roomCreated, nil
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
func (s *storeMock) JoinRoom(context.Context, ChatMemberRecord) error { return nil }
func (s *storeMock) UpsertRoomMembership(_ context.Context, member ChatMemberRecord) error {
	s.memberCreated = member
	return nil
}
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
func (s *storeMock) CreateInvitation(_ context.Context, invitation ChatInvitationRecord) error {
	s.invitation = invitation
	return nil
}
func (s *storeMock) GetInvitation(context.Context, uuid.UUID) (ChatInvitationRecord, error) {
	return s.invitation, nil
}
func (s *storeMock) ListSentInvitations(context.Context, []byte, *uuid.UUID, int, int) ([]ChatInvitationRecord, error) {
	return nil, nil
}

func (s *storeMock) ListIncomingInvitations(context.Context, []byte, int, int) ([]ChatInvitationRecord, error) {
	return nil, nil
}

func (s *storeMock) UpdateInvitationState(context.Context, uuid.UUID, []byte, int16, time.Time, []int16) (ChatInvitationRecord, error) {
	return s.invitation, nil
}
func (s *storeMock) FindPendingInvitation(context.Context, uuid.UUID, []byte) (ChatInvitationRecord, bool, error) {
	if s.invitation.InvitationID == uuid.Nil {
		return ChatInvitationRecord{}, false, nil
	}
	return s.invitation, true, nil
}
func (s *storeMock) CreateMessage(_ context.Context, message MessageRecord) error {
	s.messageCreated = message
	return nil
}
func (s *storeMock) DeleteMessage(context.Context, []byte, uuid.UUID, uuid.UUID, time.Time) error {
	return nil
}

func (s *storeMock) ListActiveRoomMemberPublicKeys(context.Context, uuid.UUID) ([][]byte, error) {
	return s.roomMembers, nil
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

func TestServiceCreateDirectRoomCreatesTwoMemberRoom(t *testing.T) {
	now := time.Date(2026, 4, 11, 16, 15, 0, 0, time.UTC)
	roomID := uuid.MustParse("abababab-abab-abab-abab-abababababab")
	alice := bytes32(1)
	bob := bytes32(2)
	store := &storeMock{
		areFriends:  true,
		keyPackages: []KeyPackageRecord{{UserPublicKey: bob, DeviceID: "device-1", KeyPackageBytes: []byte("kp"), ExpiresAt: now.Add(time.Hour)}},
	}
	service, err := NewService(Config{Version: "2", EventRetention: time.Hour}, fixedClock{now: now}, &sequenceUUID{ids: []uuid.UUID{roomID}}, noopTxManager{}, store, &eventAppenderMock{}, sessionLookupMock{})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	response, err := service.CreateDirectRoom(context.Background(), alice, bob)
	if err != nil {
		t.Fatalf("create direct room: %v", err)
	}
	if response["roomId"] != roomID || response["alreadyExisted"] != false {
		t.Fatalf("unexpected response: %#v", response)
	}
	if store.roomCreated.Visibility != VisibilityPrivate {
		t.Fatalf("unexpected visibility: %d", store.roomCreated.Visibility)
	}
	if store.directRoom.RoomID != roomID {
		t.Fatalf("expected direct room record, got %#v", store.directRoom)
	}
	if len(store.roomMembers) != 2 {
		t.Fatalf("expected two direct members, got %d", len(store.roomMembers))
	}
}

func TestServiceCreateDirectRoomReturnsExistingRoom(t *testing.T) {
	now := time.Date(2026, 4, 11, 16, 20, 0, 0, time.UTC)
	roomID := uuid.MustParse("bcbcbcbc-bcbc-bcbc-bcbc-bcbcbcbcbcbc")
	store := &storeMock{
		areFriends: true,
		directRoom: DirectRoomRecord{
			RoomID:             roomID,
			LeftUserPublicKey:  bytes32(1),
			RightUserPublicKey: bytes32(2),
			CreatedAt:          now,
		},
	}
	service, err := NewService(Config{Version: "2", EventRetention: time.Hour}, fixedClock{now: now}, &sequenceUUID{ids: []uuid.UUID{uuid.New()}}, noopTxManager{}, store, &eventAppenderMock{}, sessionLookupMock{})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	response, err := service.CreateDirectRoom(context.Background(), bytes32(1), bytes32(2))
	if err != nil {
		t.Fatalf("create direct room: %v", err)
	}
	if response["roomId"] != roomID || response["alreadyExisted"] != true {
		t.Fatalf("unexpected response: %#v", response)
	}
}

func TestServiceCreateDirectRoomRequiresFriendshipAndKeyPackages(t *testing.T) {
	now := time.Date(2026, 4, 11, 16, 25, 0, 0, time.UTC)
	service, err := NewService(Config{Version: "2", EventRetention: time.Hour}, fixedClock{now: now}, &sequenceUUID{ids: []uuid.UUID{uuid.New()}}, noopTxManager{}, &storeMock{}, &eventAppenderMock{}, sessionLookupMock{})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	if _, err := service.CreateDirectRoom(context.Background(), bytes32(1), bytes32(2)); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected forbidden without friendship, got %v", err)
	}

	store := &storeMock{areFriends: true}
	service, err = NewService(Config{Version: "2", EventRetention: time.Hour}, fixedClock{now: now}, &sequenceUUID{ids: []uuid.UUID{uuid.New()}}, noopTxManager{}, store, &eventAppenderMock{}, sessionLookupMock{})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	if _, err := service.CreateDirectRoom(context.Background(), bytes32(1), bytes32(2)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected not found without key packages, got %v", err)
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
	service, err := NewService(Config{Version: "2", EventRetention: time.Hour}, fixedClock{now: now}, &sequenceUUID{ids: []uuid.UUID{uuid.New(), uuid.New()}}, noopTxManager{}, store, &eventAppenderMock{}, sessionLookupMock{})
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
	service, err := NewService(Config{Version: "2", EventRetention: time.Hour}, fixedClock{now: now}, &sequenceUUID{ids: []uuid.UUID{uuid.New(), uuid.New()}}, noopTxManager{}, store, &eventAppenderMock{}, sessionLookupMock{})
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
	service, err := NewService(Config{Version: "2", EventRetention: time.Hour}, fixedClock{now: now}, &sequenceUUID{ids: []uuid.UUID{uuid.New(), uuid.New()}}, noopTxManager{}, store, &eventAppenderMock{}, sessionLookupMock{})
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

func TestServiceSendCommitAppendsRoomEvents(t *testing.T) {
	now := time.Date(2026, 4, 11, 21, 0, 0, 0, time.UTC)
	roomID := uuid.MustParse("66666666-6666-6666-6666-666666666666")
	actor := bytes32(1)
	memberA := bytes32(2)
	memberB := bytes32(3)
	store := &storeMock{roomMembers: [][]byte{actor, memberA, memberB}}
	events := &eventAppenderMock{}
	service, err := NewService(Config{Version: "2", EventRetention: time.Hour}, fixedClock{now: now}, &sequenceUUID{ids: []uuid.UUID{uuid.New(), uuid.New()}}, noopTxManager{}, store, events, sessionLookupMock{})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	commit := []byte("commit-1")
	response, err := service.SendCommit(context.Background(), actor, roomID, commit)
	if err != nil {
		t.Fatalf("send commit: %v", err)
	}
	if response["acceptedAt"] == nil {
		t.Fatalf("expected acceptedAt: %#v", response)
	}
	if len(events.events) != 2 {
		t.Fatalf("expected two commit events, got %d", len(events.events))
	}
	for _, event := range events.events {
		if event.EventType != "mlsCommitReceived" {
			t.Fatalf("unexpected event type: %s", event.EventType)
		}
		if event.Payload["roomId"] != roomID.String() {
			t.Fatalf("unexpected payload: %#v", event.Payload)
		}
		payloadBytes, ok := event.Payload["commitBytes"].([]byte)
		if !ok || string(payloadBytes) != string(commit) {
			t.Fatalf("unexpected payload bytes: %#v", event.Payload)
		}
	}
}

func TestServiceSendWelcomeAppendsDirectEvent(t *testing.T) {
	now := time.Date(2026, 4, 11, 21, 30, 0, 0, time.UTC)
	target := bytes32(8)
	events := &eventAppenderMock{}
	roomID := uuid.MustParse("99999999-9999-9999-9999-999999999999")
	service, err := NewService(Config{Version: "2", EventRetention: time.Hour}, fixedClock{now: now}, &sequenceUUID{ids: []uuid.UUID{uuid.New()}}, noopTxManager{}, &storeMock{roomCreated: ChatRoomRecord{RoomID: roomID}}, events, sessionLookupMock{})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	welcome := []byte("welcome-1")
	response, err := service.SendWelcome(context.Background(), bytes32(1), nil, target, welcome)
	if err != nil {
		t.Fatalf("send welcome: %v", err)
	}
	if response["acceptedAt"] == nil {
		t.Fatalf("expected acceptedAt: %#v", response)
	}
	if len(events.events) != 1 {
		t.Fatalf("expected one welcome event, got %d", len(events.events))
	}
	if events.events[0].EventType != "mlsWelcomeReceived" {
		t.Fatalf("unexpected event type: %s", events.events[0].EventType)
	}
	if string(events.events[0].UserPublicKey) != string(target) {
		t.Fatalf("unexpected event receiver: %#v", events.events[0].UserPublicKey)
	}
	payloadBytes, ok := events.events[0].Payload["welcomeBytes"].([]byte)
	if !ok || string(payloadBytes) != string(welcome) {
		t.Fatalf("unexpected welcome payload: %#v", events.events[0].Payload)
	}
}

func TestServiceUploadGroupInfoStoresLatestBytes(t *testing.T) {
	now := time.Date(2026, 4, 11, 21, 45, 0, 0, time.UTC)
	roomID := uuid.MustParse("12121212-1212-1212-1212-121212121212")
	store := &storeMock{}
	service, err := NewService(Config{Version: "2", EventRetention: time.Hour}, fixedClock{now: now}, &sequenceUUID{ids: []uuid.UUID{uuid.New()}}, noopTxManager{}, store, &eventAppenderMock{}, sessionLookupMock{})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	response, err := service.UploadGroupInfo(context.Background(), bytes32(1), roomID, []byte("group-info"))
	if err != nil {
		t.Fatalf("upload group info: %v", err)
	}
	if response["acceptedAt"] == nil {
		t.Fatalf("expected acceptedAt: %#v", response)
	}
	if store.groupInfo.RoomID != roomID || string(store.groupInfo.GroupInfoBytes) != "group-info" {
		t.Fatalf("unexpected stored group info: %#v", store.groupInfo)
	}
}

func TestServiceSendExternalCommitJoinsAndAppendsEvent(t *testing.T) {
	now := time.Date(2026, 4, 11, 21, 50, 0, 0, time.UTC)
	roomID := uuid.MustParse("13131313-1313-1313-1313-131313131313")
	actor := bytes32(1)
	member := bytes32(2)
	store := &storeMock{
		roomCreated: ChatRoomRecord{RoomID: roomID, Visibility: VisibilityPublic},
		groupInfo:   ChatRoomGroupInfoRecord{RoomID: roomID, GroupInfoBytes: []byte("group-info")},
		roomMembers: [][]byte{member},
	}
	events := &eventAppenderMock{}
	service, err := NewService(Config{Version: "2", EventRetention: time.Hour}, fixedClock{now: now}, &sequenceUUID{ids: []uuid.UUID{uuid.New(), uuid.New()}}, noopTxManager{}, store, events, sessionLookupMock{})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	response, err := service.SendExternalCommit(context.Background(), actor, roomID, []byte("commit"))
	if err != nil {
		t.Fatalf("send external commit: %v", err)
	}
	if response["acceptedAt"] == nil {
		t.Fatalf("expected acceptedAt: %#v", response)
	}
	if store.memberCreated.RoomID != roomID || string(store.memberCreated.UserPublicKey) != string(actor) {
		t.Fatalf("unexpected membership upsert: %#v", store.memberCreated)
	}
	if len(events.events) != 1 || events.events[0].EventType != "mlsExternalCommitReceived" {
		t.Fatalf("unexpected events: %#v", events.events)
	}
}

func TestServiceSendChatInvitationStoresOptionalContractFields(t *testing.T) {
	now := time.Date(2026, 4, 11, 21, 55, 0, 0, time.UTC)
	roomID := uuid.MustParse("14141414-1414-1414-1414-141414141414")
	expiresAt := now.Add(10 * time.Minute)
	store := &storeMock{}
	service, err := NewService(Config{Version: "2", EventRetention: time.Hour}, fixedClock{now: now}, &sequenceUUID{ids: []uuid.UUID{uuid.New(), uuid.New()}}, noopTxManager{}, store, &eventAppenderMock{}, sessionLookupMock{})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, err = service.SendChatInvitation(context.Background(), bytes32(1), roomID, bytes32(2), &expiresAt, []byte("token"), []byte("sig"))
	if err != nil {
		t.Fatalf("send chat invitation: %v", err)
	}
	if store.invitation.RoomID != roomID || string(store.invitation.InviteToken) != "token" || string(store.invitation.InviteTokenSignature) != "sig" {
		t.Fatalf("unexpected invitation: %#v", store.invitation)
	}
	if store.invitation.ExpiresAt == nil || !store.invitation.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("unexpected invitation expiry: %#v", store.invitation.ExpiresAt)
	}
}

func TestServiceAcceptChatInvitationAppendsExternalCommitEvent(t *testing.T) {
	now := time.Date(2026, 4, 11, 22, 10, 0, 0, time.UTC)
	roomID := uuid.MustParse("15151515-1515-1515-1515-151515151515")
	invitationID := uuid.MustParse("16161616-1616-1616-1616-161616161616")
	joiner := bytes32(3)
	inviter := bytes32(4)
	store := &storeMock{
		invitation: ChatInvitationRecord{
			InvitationID:     invitationID,
			RoomID:           roomID,
			InviterPublicKey: inviter,
			InviteePublicKey: joiner,
			State:            InvitationPending,
			CreatedAt:        now,
			UpdatedAt:        now,
		},
		roomMembers: [][]byte{inviter},
	}
	events := &eventAppenderMock{}
	service, err := NewService(Config{Version: "2", EventRetention: time.Hour}, fixedClock{now: now}, &sequenceUUID{ids: []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}}, noopTxManager{}, store, events, sessionLookupMock{})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, err = service.AcceptChatInvitation(context.Background(), joiner, invitationID, []byte("commit"))
	if err != nil {
		t.Fatalf("accept chat invitation: %v", err)
	}
	if len(events.events) != 2 {
		t.Fatalf("expected acceptance and external commit events, got %d", len(events.events))
	}
}

func TestServiceSendMessageAppendsMlsBodyEvent(t *testing.T) {
	now := time.Date(2026, 4, 11, 22, 0, 0, 0, time.UTC)
	roomID := uuid.MustParse("77777777-7777-7777-7777-777777777777")
	messageID := uuid.MustParse("88888888-8888-8888-8888-888888888888")
	actor := bytes32(1)
	member := bytes32(2)
	store := &storeMock{roomMembers: [][]byte{actor, member}}
	events := &eventAppenderMock{}
	service, err := NewService(Config{Version: "2", EventRetention: time.Hour}, fixedClock{now: now}, &sequenceUUID{ids: []uuid.UUID{messageID, uuid.New()}}, noopTxManager{}, store, events, sessionLookupMock{})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	body := [][]byte{[]byte("cipher-1"), []byte("cipher-2")}
	response, err := service.SendMessage(context.Background(), actor, roomID, uuid.New(), body)
	if err != nil {
		t.Fatalf("send message: %v", err)
	}
	if response["messageId"] != messageID {
		t.Fatalf("unexpected message id: %#v", response["messageId"])
	}
	if len(events.events) != 1 {
		t.Fatalf("expected one message event, got %d", len(events.events))
	}
	if events.events[0].EventType != "mlsMessageReceived" {
		t.Fatalf("unexpected event type: %s", events.events[0].EventType)
	}
	rawBody, ok := events.events[0].Payload["body"].([][]byte)
	if !ok || len(rawBody) != 2 || string(rawBody[0]) != "cipher-1" || string(rawBody[1]) != "cipher-2" {
		t.Fatalf("unexpected body payload: %#v", events.events[0].Payload["body"])
	}
}

func bytes32(value byte) []byte {
	out := make([]byte, 32)
	for i := range out {
		out[i] = value
	}
	return out
}
