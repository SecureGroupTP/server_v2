package clientapi

import (
	"context"
	"time"

	"github.com/google/uuid"

	domainauth "server_v2/internal/domain/auth"
)

const (
	VisibilityPublic   = 1
	VisibilityLinkOnly = 2
	VisibilityPrivate  = 3

	RoleMember = 1
	RoleAdmin  = 2
	RoleOwner  = 3

	NotificationAll          = 1
	NotificationMentionsOnly = 2
	NotificationMuted        = 3

	FriendRequestPending  = 1
	FriendRequestAccepted = 2
	FriendRequestDeclined = 3
	FriendRequestCanceled = 4

	InvitationPending  = 1
	InvitationAccepted = 2
	InvitationDeclined = 3
	InvitationRevoked  = 4
)

type Clock interface {
	Now() time.Time
}

type UUIDGenerator interface {
	New() uuid.UUID
}

type EventAppender interface {
	Append(ctx context.Context, event domainauth.Event) error
}

type SessionLookup interface {
	LookupSession(ctx context.Context, sessionID uuid.UUID) (domainauth.Session, error)
}

type Store interface {
	GetProfile(ctx context.Context, publicKey []byte) (ProfileRecord, error)
	GetActiveBanStatus(ctx context.Context, publicKey []byte, now time.Time) (BanStatusRecord, bool, error)
	UpdateProfile(ctx context.Context, publicKey []byte, username string, displayName string, avatarHash string, bio string, updatedAt time.Time) error
	SearchProfiles(ctx context.Context, query string, limit int, offset int) ([]ProfileRecord, error)
	DeleteAccount(ctx context.Context, publicKey []byte, deletedAt time.Time) error
	GetProfileAvatar(ctx context.Context, publicKey []byte) (AvatarRecord, error)

	ListDevices(ctx context.Context, userPublicKey []byte) ([]DeviceRecord, error)
	UpsertDevice(ctx context.Context, device DeviceRecord) (DeviceRecord, error)
	RemoveDevice(ctx context.Context, userPublicKey []byte, deviceID uuid.UUID, removedAt time.Time) error

	InsertKeyPackages(ctx context.Context, keyPackages []KeyPackageRecord) (int, error)
	FetchKeyPackages(ctx context.Context, userPublicKeys [][]byte, now time.Time) ([]KeyPackageRecord, error)
	DeleteKeyPackagesByUserDevice(ctx context.Context, userPublicKey []byte, deviceID string) error
	UpsertRoomGroupInfo(ctx context.Context, userPublicKey []byte, groupInfo ChatRoomGroupInfoRecord) error
	GetRoomGroupInfo(ctx context.Context, roomID uuid.UUID) (ChatRoomGroupInfoRecord, error)
	FindDirectRoomIDByUsers(ctx context.Context, leftUserPublicKey []byte, rightUserPublicKey []byte) (uuid.UUID, bool, error)
	CreateDirectRoom(ctx context.Context, room ChatRoomRecord, left ChatMemberRecord, right ChatMemberRecord, direct DirectRoomRecord) error
	IsDirectRoom(ctx context.Context, roomID uuid.UUID) (bool, error)
	UpsertRoomWelcome(ctx context.Context, welcome ChatRoomWelcomeRecord) error
	GetRoomWelcome(ctx context.Context, roomID uuid.UUID, targetUserPublicKey []byte) (ChatRoomWelcomeRecord, error)

	AreFriends(ctx context.Context, leftUserPublicKey []byte, rightUserPublicKey []byte) (bool, error)
	ListFriends(ctx context.Context, userPublicKey []byte, limit int, offset int) ([]FriendRecord, error)
	CountFriends(ctx context.Context, userPublicKey []byte) (int64, error)
	RemoveFriend(ctx context.Context, userPublicKey []byte, friendPublicKey []byte, removedAt time.Time) error
	CreateFriendRequest(ctx context.Context, request FriendRequestRecord) error
	UpdateFriendRequestState(ctx context.Context, requestID uuid.UUID, actorPublicKey []byte, allowedFromStates []int16, targetState int16, updatedAt time.Time) (FriendRequestRecord, error)
	GetFriendRequest(ctx context.Context, requestID uuid.UUID) (FriendRequestRecord, error)
	ListFriendRequests(ctx context.Context, userPublicKey []byte, direction string, limit int, offset int) ([]FriendRequestRecord, error)
	CreateFriendPair(ctx context.Context, left FriendRecord, right FriendRecord) error

	CreateRoom(ctx context.Context, room ChatRoomRecord, owner ChatMemberRecord) error
	ListRooms(ctx context.Context, userPublicKey []byte, limit int, offset int) ([]ChatRoomRecord, error)
	GetRoom(ctx context.Context, roomID uuid.UUID) (ChatRoomRecord, error)
	SearchRooms(ctx context.Context, query string, limit int, offset int) ([]ChatRoomRecord, error)
	UpdateRoom(ctx context.Context, userPublicKey []byte, roomID uuid.UUID, title string, description string, avatarHash string, updatedAt time.Time) error
	DeleteRoom(ctx context.Context, userPublicKey []byte, roomID uuid.UUID, deletedAt time.Time) error
	GetRoomAvatar(ctx context.Context, roomID uuid.UUID) (AvatarRecord, error)
	AddRoomState(ctx context.Context, userPublicKey []byte, state ChatRoomStateRecord) error
	FetchRoomState(ctx context.Context, userPublicKey []byte, roomID uuid.UUID, epoch int64) (ChatRoomStateRecord, error)
	JoinRoom(ctx context.Context, member ChatMemberRecord) error
	UpsertRoomMembership(ctx context.Context, member ChatMemberRecord) error
	LeaveRoom(ctx context.Context, roomID uuid.UUID, userPublicKey []byte, leftAt time.Time) error
	KickMember(ctx context.Context, actorPublicKey []byte, roomID uuid.UUID, targetPublicKey []byte, kickedAt time.Time) error
	ListMembers(ctx context.Context, roomID uuid.UUID, limit int, offset int) ([]ChatMemberRecord, error)
	UpdateMemberRole(ctx context.Context, actorPublicKey []byte, roomID uuid.UUID, targetPublicKey []byte, role int16, updatedAt time.Time) error
	CreateMemberPermission(ctx context.Context, actorPublicKey []byte, permission ChatMemberPermissionRecord) error
	ListMemberPermissions(ctx context.Context, roomID uuid.UUID, userPublicKey []byte, limit int, offset int) ([]ChatMemberPermissionRecord, error)
	UpdateMemberPermission(ctx context.Context, actorPublicKey []byte, permissionID uuid.UUID, isAllowed bool, updatedAt time.Time) error
	DeleteMemberPermission(ctx context.Context, actorPublicKey []byte, permissionID uuid.UUID, deletedAt time.Time) error
	CreateInvitation(ctx context.Context, invitation ChatInvitationRecord) error
	GetInvitation(ctx context.Context, invitationID uuid.UUID) (ChatInvitationRecord, error)
	ListSentInvitations(ctx context.Context, inviterPublicKey []byte, roomID *uuid.UUID, limit int, offset int) ([]ChatInvitationRecord, error)
	ListIncomingInvitations(ctx context.Context, inviteePublicKey []byte, limit int, offset int) ([]ChatInvitationRecord, error)
	UpdateInvitationState(ctx context.Context, invitationID uuid.UUID, actorPublicKey []byte, targetState int16, updatedAt time.Time, allowedCurrentStates []int16) (ChatInvitationRecord, error)
	FindPendingInvitation(ctx context.Context, roomID uuid.UUID, inviteePublicKey []byte) (ChatInvitationRecord, bool, error)
	CreateMessage(ctx context.Context, message MessageRecord) error
	DeleteMessage(ctx context.Context, actorPublicKey []byte, roomID uuid.UUID, messageID uuid.UUID, deletedAt time.Time) error
	ListActiveRoomMemberPublicKeys(ctx context.Context, roomID uuid.UUID) ([][]byte, error)

	CountServerStats(ctx context.Context) (ServerStats, error)
	CountUserStats(ctx context.Context, userPublicKey []byte) (UserStats, error)
	CountGroupStats(ctx context.Context, roomID uuid.UUID) (GroupStats, error)
}

type ProfileRecord struct {
	PublicKey   []byte
	Username    string
	DisplayName string
	Bio         string
	AvatarHash  string
	AvatarBytes []byte
	ContentType string
	LastSeenAt  time.Time
	UpdatedAt   time.Time
	DeletedAt   *time.Time
}

type AvatarRecord struct {
	Bytes       []byte
	ContentType string
}

type BanStatusRecord struct {
	IsBanned  bool
	Reason    string
	BannedAt  time.Time
	ExpiresAt *time.Time
}

type DeviceRecord struct {
	ID            uuid.UUID
	SessionID     *uuid.UUID
	UserPublicKey []byte
	DeviceID      string
	Platform      int16
	PushToken     string
	IsEnabled     bool
	UpdatedAt     time.Time
}

type KeyPackageRecord struct {
	ID              uuid.UUID
	UserPublicKey   []byte
	DeviceID        string
	KeyPackageBytes []byte
	IsLastResort    bool
	CreatedAt       time.Time
	ExpiresAt       time.Time
}

type ChatRoomGroupInfoRecord struct {
	RoomID            uuid.UUID
	UploaderPublicKey []byte
	GroupInfoBytes    []byte
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type ChatRoomWelcomeRecord struct {
	RoomID              uuid.UUID
	TargetUserPublicKey []byte
	SenderPublicKey     []byte
	WelcomeBytes        []byte
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type DirectRoomRecord struct {
	RoomID             uuid.UUID
	LeftUserPublicKey  []byte
	RightUserPublicKey []byte
	CreatedAt          time.Time
}

type FriendRecord struct {
	ID              uuid.UUID
	UserPublicKey   []byte
	FriendPublicKey []byte
	AcceptedAt      time.Time
}

type FriendRequestRecord struct {
	RequestID         uuid.UUID
	SenderPublicKey   []byte
	ReceiverPublicKey []byte
	State             int16
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type ChatRoomRecord struct {
	RoomID            uuid.UUID
	OwnerPublicKey    []byte
	Title             string
	Description       string
	Visibility        int16
	AvatarHash        string
	AvatarBytes       []byte
	AvatarContentType string
	StateID           *uuid.UUID
	CreatedAt         time.Time
	UpdatedAt         time.Time
	DeletedAt         *time.Time
}

type ChatRoomStateRecord struct {
	ID        uuid.UUID
	RoomID    uuid.UUID
	GroupID   uuid.UUID
	Epoch     int64
	TreeBytes []byte
	TreeHash  []byte
	CreatedAt time.Time
}

type ChatMemberRecord struct {
	RoomID            uuid.UUID
	UserPublicKey     []byte
	Role              int16
	NotificationLevel int16
	JoinedAt          time.Time
	LeftAt            *time.Time
}

type ChatMemberPermissionRecord struct {
	PermissionID  uuid.UUID
	RoomID        uuid.UUID
	UserPublicKey []byte
	PermissionKey string
	IsAllowed     bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type ChatInvitationRecord struct {
	InvitationID         uuid.UUID
	RoomID               uuid.UUID
	InviterPublicKey     []byte
	InviteePublicKey     []byte
	ExpiresAt            *time.Time
	InviteToken          []byte
	InviteTokenSignature []byte
	State                int16
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type MessageRecord struct {
	MessageID       uuid.UUID
	RoomID          uuid.UUID
	SenderPublicKey []byte
	ClientMsgID     uuid.UUID
	Body            [][]byte
	CreatedAt       time.Time
	DeletedAt       *time.Time
}

type ServerStats struct {
	Profiles int64
	Devices  int64
	Friends  int64
	Rooms    int64
	Messages int64
}

type UserStats struct {
	Devices                int64
	KeyPackages            int64
	Friends                int64
	OutgoingFriendRequests int64
	Rooms                  int64
}

type GroupStats struct {
	Members  int64
	Messages int64
	Invites  int64
}
