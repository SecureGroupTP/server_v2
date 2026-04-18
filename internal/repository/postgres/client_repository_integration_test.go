package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	clientapi "server_v2/internal/application/clientapi"
	domainauth "server_v2/internal/domain/auth"
)

func TestClientRepositoryRoomMemberStats(t *testing.T) {
	store := openTestStore(t)
	repo := NewClientRepository(store.DB())
	now := time.Date(2026, 4, 11, 17, 0, 0, 0, time.UTC)
	user1 := []byte("11111111111111111111111111111111")
	user2 := []byte("22222222222222222222222222222222")
	roomID := uuid.New()

	if err := repo.UpdateProfile(context.Background(), user1, "@alice", "Alice", "", "bio", now); err != nil {
		t.Fatalf("update profile 1: %v", err)
	}
	user1Profile, err := repo.GetProfile(context.Background(), user1)
	if err != nil {
		t.Fatalf("get profile 1: %v", err)
	}
	if user1Profile.Username != "@alice" {
		t.Fatalf("unexpected username: %q", user1Profile.Username)
	}
	if err := repo.UpdateProfile(context.Background(), user2, "@bob", "Bob", "", "bio", now); err != nil {
		t.Fatalf("update profile 2: %v", err)
	}
	if err := repo.CreateRoom(context.Background(), clientapi.ChatRoomRecord{RoomID: roomID, OwnerPublicKey: user1, Title: "room", Visibility: clientapi.VisibilityPublic, CreatedAt: now, UpdatedAt: now}, clientapi.ChatMemberRecord{RoomID: roomID, UserPublicKey: user1, Role: clientapi.RoleOwner, NotificationLevel: clientapi.NotificationAll, JoinedAt: now}); err != nil {
		t.Fatalf("create room: %v", err)
	}
	if err := repo.JoinRoom(context.Background(), clientapi.ChatMemberRecord{RoomID: roomID, UserPublicKey: user2, Role: clientapi.RoleMember, NotificationLevel: clientapi.NotificationAll, JoinedAt: now}); err != nil {
		t.Fatalf("join room: %v", err)
	}
	stats, err := repo.CountGroupStats(context.Background(), roomID)
	if err != nil {
		t.Fatalf("group stats: %v", err)
	}
	if stats.Members != 2 || stats.Invites != 0 {
		t.Fatalf("unexpected stats: %#v", stats)
	}

	members, err := repo.ListActiveRoomMemberPublicKeys(context.Background(), roomID)
	if err != nil {
		t.Fatalf("list members: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("expected 2 members, got %d", len(members))
	}
}

func TestClientRepositorySearchProfilesPaginationAndBanStatus(t *testing.T) {
	store := openTestStore(t)
	repo := NewClientRepository(store.DB())
	now := time.Date(2026, 4, 11, 18, 30, 0, 0, time.UTC)
	user1 := []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	user2 := []byte("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	user3 := []byte("cccccccccccccccccccccccccccccccc")

	for _, item := range []struct {
		key         []byte
		username    string
		displayName string
	}{
		{user1, "@alice", "Alice"},
		{user2, "@alicia", "Alicia"},
		{user3, "@alina", "Alina"},
	} {
		if err := repo.UpdateProfile(context.Background(), item.key, item.username, item.displayName, "", "bio", now); err != nil {
			t.Fatalf("update profile %q: %v", item.displayName, err)
		}
		now = now.Add(time.Second)
	}

	page1, err := repo.SearchProfiles(context.Background(), "ali", 2, 0)
	if err != nil {
		t.Fatalf("search profiles page1: %v", err)
	}
	if len(page1) != 2 {
		t.Fatalf("expected 2 profiles on page1, got %d", len(page1))
	}

	page2, err := repo.SearchProfiles(context.Background(), "ali", 2, 2)
	if err != nil {
		t.Fatalf("search profiles page2: %v", err)
	}
	if len(page2) != 1 {
		t.Fatalf("expected 1 profile on page2, got %d", len(page2))
	}

	expiresAt := now.Add(time.Hour)
	if _, err := store.DB().ExecContext(context.Background(), `INSERT INTO ban_statuses (public_key, is_banned, reason, banned_at, expires_at, updated_at) VALUES ($1, TRUE, $2, $3, $4, $3)`, user1, "moderation", now, expiresAt); err != nil {
		t.Fatalf("insert ban status: %v", err)
	}

	banStatus, found, err := repo.GetActiveBanStatus(context.Background(), user1, now)
	if err != nil {
		t.Fatalf("get active ban status: %v", err)
	}
	if !found || !banStatus.IsBanned || banStatus.Reason != "moderation" {
		t.Fatalf("unexpected ban status: found=%v status=%#v", found, banStatus)
	}
}

func TestClientRepositoryMlsArtifactsAndInvitationContract(t *testing.T) {
	store := openTestStore(t)
	repo := NewClientRepository(store.DB())
	now := time.Date(2026, 4, 11, 19, 0, 0, 0, time.UTC)
	user1 := []byte("dddddddddddddddddddddddddddddddd")
	user2 := []byte("eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee")
	roomID := uuid.New()
	invitationID := uuid.New()
	expiresAt := now.Add(15 * time.Minute)

	if err := repo.UpdateProfile(context.Background(), user1, "@dora", "Dora", "", "bio", now); err != nil {
		t.Fatalf("update profile 1: %v", err)
	}
	if err := repo.UpdateProfile(context.Background(), user2, "@eric", "Eric", "", "bio", now); err != nil {
		t.Fatalf("update profile 2: %v", err)
	}
	leftKey, rightKey := orderedPublicKeyPair(user1, user2)
	if err := repo.CreateDirectRoom(
		context.Background(),
		clientapi.ChatRoomRecord{RoomID: roomID, OwnerPublicKey: user1, Title: "direct", Visibility: clientapi.VisibilityPrivate, CreatedAt: now, UpdatedAt: now},
		clientapi.ChatMemberRecord{RoomID: roomID, UserPublicKey: user1, Role: clientapi.RoleOwner, NotificationLevel: clientapi.NotificationAll, JoinedAt: now},
		clientapi.ChatMemberRecord{RoomID: roomID, UserPublicKey: user2, Role: clientapi.RoleMember, NotificationLevel: clientapi.NotificationAll, JoinedAt: now},
		clientapi.DirectRoomRecord{RoomID: roomID, LeftUserPublicKey: leftKey, RightUserPublicKey: rightKey, CreatedAt: now},
	); err != nil {
		t.Fatalf("create direct room: %v", err)
	}
	if err := repo.UpsertRoomGroupInfo(context.Background(), user1, clientapi.ChatRoomGroupInfoRecord{
		RoomID:            roomID,
		UploaderPublicKey: user1,
		GroupInfoBytes:    []byte("group-info"),
		CreatedAt:         now,
		UpdatedAt:         now,
	}); err != nil {
		t.Fatalf("upsert group info: %v", err)
	}
	groupInfo, err := repo.GetRoomGroupInfo(context.Background(), roomID)
	if err != nil {
		t.Fatalf("get group info: %v", err)
	}
	if string(groupInfo.GroupInfoBytes) != "group-info" {
		t.Fatalf("unexpected group info: %#v", groupInfo)
	}

	foundRoomID, found, err := repo.FindDirectRoomIDByUsers(context.Background(), user1, user2)
	if err != nil {
		t.Fatalf("find direct room: %v", err)
	}
	if !found || foundRoomID != roomID {
		t.Fatalf("unexpected direct room lookup: found=%v room=%s", found, foundRoomID)
	}
	if direct, err := repo.IsDirectRoom(context.Background(), roomID); err != nil || !direct {
		t.Fatalf("expected direct room, direct=%v err=%v", direct, err)
	}

	// Welcomes are stored as user events ("outbox") so clients can re-fetch them by room id.
	authRepo := NewAuthRepository(store.DB())
	eventNow := time.Now().UTC()
	if err := authRepo.Append(context.Background(), domainauth.Event{
		EventID:       uuid.New(),
		UserPublicKey: user2,
		EventType:     "mlsWelcomeReceived",
		Payload: map[string]any{
			"roomId":              roomID.String(),
			"targetUserPublicKey": user2,
			"senderPublicKey":     user1,
			"welcomeBytes":        []byte("welcome"),
		},
		AvailableAt: eventNow.Add(-time.Second),
		ExpiresAt:   eventNow.Add(time.Hour),
		CreatedAt:   eventNow,
	}); err != nil {
		t.Fatalf("append welcome event: %v", err)
	}
	welcome, err := repo.GetRoomWelcome(context.Background(), roomID, user2)
	if err != nil {
		t.Fatalf("get room welcome: %v", err)
	}
	if string(welcome.WelcomeBytes) != "welcome" {
		t.Fatalf("unexpected welcome: %#v", welcome)
	}

	if err := repo.CreateInvitation(context.Background(), clientapi.ChatInvitationRecord{
		InvitationID:         invitationID,
		RoomID:               roomID,
		InviterPublicKey:     user1,
		InviteePublicKey:     user2,
		ExpiresAt:            &expiresAt,
		InviteToken:          []byte("token"),
		InviteTokenSignature: []byte("sig"),
		State:                clientapi.InvitationPending,
		CreatedAt:            now,
		UpdatedAt:            now,
	}); err != nil {
		t.Fatalf("create invitation: %v", err)
	}
	invitation, err := repo.GetInvitation(context.Background(), invitationID)
	if err != nil {
		t.Fatalf("get invitation: %v", err)
	}
	if invitation.ExpiresAt == nil || string(invitation.InviteToken) != "token" || string(invitation.InviteTokenSignature) != "sig" {
		t.Fatalf("unexpected invitation payload: %#v", invitation)
	}
}

func TestClientRepositoryBroadCRUDCoverage(t *testing.T) {
	store := openTestStore(t)
	repo := NewClientRepository(store.DB())
	ctx := context.Background()
	now := time.Date(2026, 4, 12, 9, 0, 0, 0, time.UTC)
	user1 := []byte("ffffffffffffffffffffffffffffffff")
	user2 := []byte("gggggggggggggggggggggggggggggggg")
	user3 := []byte("hhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhh")
	roomID := uuid.New()
	groupID := uuid.New()
	stateID := uuid.New()
	permissionID := uuid.New()
	invitationID := uuid.New()
	deviceUUID := uuid.New()
	expiresAt := now.Add(time.Hour)

	for _, item := range []struct {
		key      []byte
		username string
		name     string
	}{
		{user1, "@owner", "Owner"},
		{user2, "@friend", "Friend"},
		{user3, "@joiner", "Joiner"},
	} {
		if err := repo.UpdateProfile(ctx, item.key, item.username, item.name, "", "bio", now); err != nil {
			t.Fatalf("update profile %s: %v", item.username, err)
		}
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE profiles SET avatar_bytes = $2, avatar_content_type = $3 WHERE public_key = $1`, user1, []byte("profile-avatar"), "image/png"); err != nil {
		t.Fatalf("seed profile avatar: %v", err)
	}
	avatar, err := repo.GetProfileAvatar(ctx, user1)
	if err != nil {
		t.Fatalf("get profile avatar: %v", err)
	}
	if string(avatar.Bytes) != "profile-avatar" || avatar.ContentType != "image/png" {
		t.Fatalf("unexpected profile avatar: %#v", avatar)
	}

	device, err := repo.UpsertDevice(ctx, clientapi.DeviceRecord{
		ID:            deviceUUID,
		UserPublicKey: user1,
		DeviceID:      "device-1",
		Platform:      1,
		PushToken:     "push",
		IsEnabled:     true,
		UpdatedAt:     now,
	})
	if err != nil {
		t.Fatalf("upsert device: %v", err)
	}
	if device.ID != deviceUUID {
		t.Fatalf("unexpected device: %#v", device)
	}
	devices, err := repo.ListDevices(ctx, user1)
	if err != nil || len(devices) != 1 {
		t.Fatalf("list devices: len=%d err=%v", len(devices), err)
	}

	if count, err := repo.InsertKeyPackages(ctx, []clientapi.KeyPackageRecord{
		{ID: uuid.New(), UserPublicKey: user1, DeviceID: "device-1", KeyPackageBytes: []byte("kp-1"), CreatedAt: now, ExpiresAt: expiresAt},
		{ID: uuid.New(), UserPublicKey: user2, DeviceID: "device-2", KeyPackageBytes: []byte("kp-2"), IsLastResort: true, CreatedAt: now, ExpiresAt: expiresAt},
	}); err != nil || count != 2 {
		t.Fatalf("insert key packages: count=%d err=%v", count, err)
	}
	packages, err := repo.FetchKeyPackages(ctx, [][]byte{user1, user2}, now)
	if err != nil || len(packages) != 2 {
		t.Fatalf("fetch key packages: len=%d err=%v", len(packages), err)
	}
	if err := repo.DeleteKeyPackagesByUserDevice(ctx, user2, "device-2"); err != nil {
		t.Fatalf("delete key packages by device: %v", err)
	}

	requestID := uuid.New()
	if err := repo.CreateFriendRequest(ctx, clientapi.FriendRequestRecord{
		RequestID:         requestID,
		SenderPublicKey:   user1,
		ReceiverPublicKey: user2,
		State:             clientapi.FriendRequestPending,
		CreatedAt:         now,
		UpdatedAt:         now,
	}); err != nil {
		t.Fatalf("create friend request: %v", err)
	}
	if _, err := repo.GetFriendRequest(ctx, requestID); err != nil {
		t.Fatalf("get friend request: %v", err)
	}
	if incoming, err := repo.ListFriendRequests(ctx, user2, "incoming", 10, 0); err != nil || len(incoming) != 1 {
		t.Fatalf("list incoming friend requests: len=%d err=%v", len(incoming), err)
	}
	if outgoing, err := repo.ListFriendRequests(ctx, user1, "outgoing", 10, 0); err != nil || len(outgoing) != 1 {
		t.Fatalf("list outgoing friend requests: len=%d err=%v", len(outgoing), err)
	}
	updatedRequest, err := repo.UpdateFriendRequestState(ctx, requestID, user2, []int16{clientapi.FriendRequestPending}, clientapi.FriendRequestAccepted, now)
	if err != nil {
		t.Fatalf("update friend request: %v", err)
	}
	if updatedRequest.State != clientapi.FriendRequestAccepted {
		t.Fatalf("unexpected friend request state: %#v", updatedRequest)
	}
	if err := repo.CreateFriendPair(ctx,
		clientapi.FriendRecord{ID: uuid.New(), UserPublicKey: user1, FriendPublicKey: user2, AcceptedAt: now},
		clientapi.FriendRecord{ID: uuid.New(), UserPublicKey: user2, FriendPublicKey: user1, AcceptedAt: now},
	); err != nil {
		t.Fatalf("create friend pair: %v", err)
	}
	areFriends, err := repo.AreFriends(ctx, user1, user2)
	if err != nil || !areFriends {
		t.Fatalf("are friends=%v err=%v", areFriends, err)
	}
	if friends, err := repo.ListFriends(ctx, user1, 10, 0); err != nil || len(friends) != 1 {
		t.Fatalf("list friends: len=%d err=%v", len(friends), err)
	}
	if count, err := repo.CountFriends(ctx, user1); err != nil || count != 1 {
		t.Fatalf("count friends: count=%d err=%v", count, err)
	}

	if err := repo.CreateRoom(ctx,
		clientapi.ChatRoomRecord{
			RoomID:            roomID,
			OwnerPublicKey:    user1,
			Title:             "Coverage Room",
			Description:       "desc",
			Visibility:        clientapi.VisibilityPublic,
			AvatarHash:        "avatar-hash",
			AvatarBytes:       []byte("room-avatar"),
			AvatarContentType: "image/jpeg",
			CreatedAt:         now,
			UpdatedAt:         now,
		},
		clientapi.ChatMemberRecord{RoomID: roomID, UserPublicKey: user1, Role: clientapi.RoleOwner, NotificationLevel: clientapi.NotificationAll, JoinedAt: now},
	); err != nil {
		t.Fatalf("create room: %v", err)
	}
	if rooms, err := repo.ListRooms(ctx, user1, 10, 0); err != nil || len(rooms) != 1 {
		t.Fatalf("list rooms: len=%d err=%v", len(rooms), err)
	}
	if rooms, err := repo.SearchRooms(ctx, "coverage", 10, 0); err != nil || len(rooms) != 1 {
		t.Fatalf("search rooms: len=%d err=%v", len(rooms), err)
	}
	if err := repo.UpdateRoom(ctx, user1, roomID, "Renamed", "new desc", "new-hash", now.Add(time.Minute)); err != nil {
		t.Fatalf("update room: %v", err)
	}
	room, err := repo.GetRoom(ctx, roomID)
	if err != nil || room.Title != "Renamed" {
		t.Fatalf("get updated room: room=%#v err=%v", room, err)
	}
	roomAvatar, err := repo.GetRoomAvatar(ctx, roomID)
	if err != nil || string(roomAvatar.Bytes) != "room-avatar" || roomAvatar.ContentType != "image/jpeg" {
		t.Fatalf("get room avatar: avatar=%#v err=%v", roomAvatar, err)
	}
	if err := repo.AddRoomState(ctx, user1, clientapi.ChatRoomStateRecord{ID: stateID, RoomID: roomID, GroupID: groupID, Epoch: 1, TreeBytes: []byte("tree"), TreeHash: []byte("hash"), CreatedAt: now}); err != nil {
		t.Fatalf("add room state: %v", err)
	}
	state, err := repo.FetchRoomState(ctx, user1, roomID, 1)
	if err != nil || state.ID != stateID {
		t.Fatalf("fetch room state: state=%#v err=%v", state, err)
	}

	if err := repo.JoinRoom(ctx, clientapi.ChatMemberRecord{RoomID: roomID, UserPublicKey: user2, Role: clientapi.RoleMember, NotificationLevel: clientapi.NotificationAll, JoinedAt: now}); err != nil {
		t.Fatalf("join room: %v", err)
	}
	if err := repo.UpsertRoomMembership(ctx, clientapi.ChatMemberRecord{RoomID: roomID, UserPublicKey: user3, Role: clientapi.RoleMember, NotificationLevel: clientapi.NotificationMuted, JoinedAt: now}); err != nil {
		t.Fatalf("upsert room membership: %v", err)
	}
	if err := repo.UpdateMemberRole(ctx, user1, roomID, user2, clientapi.RoleAdmin, now); err != nil {
		t.Fatalf("update member role: %v", err)
	}
	if members, err := repo.ListMembers(ctx, roomID, 10, 0); err != nil || len(members) != 3 {
		t.Fatalf("list members: len=%d err=%v", len(members), err)
	}
	if err := repo.CreateMemberPermission(ctx, user1, clientapi.ChatMemberPermissionRecord{PermissionID: permissionID, RoomID: roomID, UserPublicKey: user2, PermissionKey: "send", IsAllowed: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create member permission: %v", err)
	}
	if permissions, err := repo.ListMemberPermissions(ctx, roomID, user2, 10, 0); err != nil || len(permissions) != 1 {
		t.Fatalf("list member permissions: len=%d err=%v", len(permissions), err)
	}
	if err := repo.UpdateMemberPermission(ctx, user1, permissionID, false, now); err != nil {
		t.Fatalf("update member permission: %v", err)
	}
	if err := repo.DeleteMemberPermission(ctx, user1, permissionID, now); err != nil {
		t.Fatalf("delete member permission: %v", err)
	}

	if err := repo.CreateInvitation(ctx, clientapi.ChatInvitationRecord{InvitationID: invitationID, RoomID: roomID, InviterPublicKey: user1, InviteePublicKey: user3, ExpiresAt: &expiresAt, State: clientapi.InvitationPending, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create invitation: %v", err)
	}
	if foundInvitation, found, err := repo.FindPendingInvitation(ctx, roomID, user3); err != nil || !found || foundInvitation.InvitationID != invitationID {
		t.Fatalf("find pending invitation: invitation=%#v found=%v err=%v", foundInvitation, found, err)
	}
	if sent, err := repo.ListSentInvitations(ctx, user1, &roomID, 10, 0); err != nil || len(sent) != 1 {
		t.Fatalf("list sent invitations: len=%d err=%v", len(sent), err)
	}
	if incoming, err := repo.ListIncomingInvitations(ctx, user3, 10, 0); err != nil || len(incoming) != 1 {
		t.Fatalf("list incoming invitations: len=%d err=%v", len(incoming), err)
	}
	if updatedInvitation, err := repo.UpdateInvitationState(ctx, invitationID, user3, clientapi.InvitationAccepted, now, []int16{clientapi.InvitationPending}); err != nil || updatedInvitation.State != clientapi.InvitationAccepted {
		t.Fatalf("update invitation: invitation=%#v err=%v", updatedInvitation, err)
	}

	if stats, err := repo.CountServerStats(ctx); err != nil || stats.Profiles != 3 || stats.Rooms != 1 {
		t.Fatalf("server stats: stats=%#v err=%v", stats, err)
	}
	if stats, err := repo.CountUserStats(ctx, user1); err != nil || stats.Devices != 1 || stats.Friends != 1 || stats.Rooms != 1 {
		t.Fatalf("user stats: stats=%#v err=%v", stats, err)
	}
	if stats, err := repo.CountGroupStats(ctx, roomID); err != nil || stats.Members != 3 || stats.Invites != 1 {
		t.Fatalf("group stats: stats=%#v err=%v", stats, err)
	}

	if err := repo.LeaveRoom(ctx, roomID, user3, now); err != nil {
		t.Fatalf("leave room: %v", err)
	}
	if err := repo.KickMember(ctx, user1, roomID, user2, now); err != nil {
		t.Fatalf("kick member: %v", err)
	}
	if err := repo.RemoveFriend(ctx, user1, user2, now); err != nil {
		t.Fatalf("remove friend: %v", err)
	}
	areFriends, err = repo.AreFriends(ctx, user1, user2)
	if err != nil || areFriends {
		t.Fatalf("expected friendship removed, areFriends=%v err=%v", areFriends, err)
	}
	if err := repo.RemoveDevice(ctx, user1, deviceUUID, now); err != nil {
		t.Fatalf("remove device: %v", err)
	}
	if err := repo.DeleteAccount(ctx, user3, now); err != nil {
		t.Fatalf("delete account: %v", err)
	}
	deletedProfile, err := repo.GetProfile(ctx, user3)
	if err != nil {
		t.Fatalf("get deleted profile marker: %v", err)
	}
	if deletedProfile.DeletedAt == nil {
		t.Fatalf("expected deleted profile marker, got %#v", deletedProfile)
	}
}
