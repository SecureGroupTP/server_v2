package push

import "time"

const (
	KindMessage       = "message"
	KindFriendRequest = "friendRequest"
)

type SafePayload struct {
	Title        string
	Subtitle     string
	SenderID     string
	SenderName   string
	PeerID       string
	DisplayName  string
	MessageCount int
}

type Envelope struct {
	EventID     string
	EventType   string
	Kind        string
	DeviceID    string
	SegmentID   string
	RoomID      string
	CreatedAt   time.Time
	SafePayload SafePayload
}

type TargetDevice struct {
	DeviceID  string
	Platform  int16
	PushToken string
	IsEnabled bool
	Found     bool
}
