package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	clientapi "server_v2/internal/application/clientapi"
)

func TestClientRepositoryRoomMessageStats(t *testing.T) {
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
	if err := repo.CreateMessage(context.Background(), clientapi.MessageRecord{MessageID: uuid.New(), RoomID: roomID, SenderPublicKey: user1, ClientMsgID: uuid.New(), Body: [][]byte{[]byte("hello")}, CreatedAt: now}); err != nil {
		t.Fatalf("create message: %v", err)
	}

	stats, err := repo.CountGroupStats(context.Background(), roomID)
	if err != nil {
		t.Fatalf("group stats: %v", err)
	}
	if stats.Members != 2 || stats.Messages != 1 {
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
