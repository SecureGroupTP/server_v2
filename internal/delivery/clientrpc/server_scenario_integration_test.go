package clientrpc

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	appauth "server_v2/internal/application/auth"
	clientapi "server_v2/internal/application/clientapi"
	"server_v2/internal/domain/rpc"
	"server_v2/internal/platform/clock"
	"server_v2/internal/platform/randombytes"
	"server_v2/internal/platform/uuidx"
	"server_v2/internal/repository/postgres"
	appserver "server_v2/internal/server"
	"server_v2/internal/testutil/postgrestest"
)

func TestServerScenarioProfileFriendRoomMessage(t *testing.T) {
	dsn := postgrestest.DSN(t)
	store, err := postgres.Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() {
		_ = store.Close()
	}()
	postgrestest.CleanupTables(t, store)

	authRepo := postgres.NewAuthRepository(store.DB())
	clientRepo := postgres.NewClientRepository(store.DB())
	txManager := postgres.NewTxManager(store.DB())
	authService, err := appauth.NewService(appauth.Config{ChallengeTTL: 5 * time.Minute, EventRetention: time.Hour, EventBatchSize: 100}, clock.Real{}, uuidx.DefaultGenerator{}, randombytes.CryptoReader{}, txManager, authRepo, authRepo, authRepo)
	if err != nil {
		t.Fatalf("new auth service: %v", err)
	}
	clientService, err := clientapi.NewService(clientapi.Config{AppName: "server-v2", Version: "2", SessionChallengeTTL: 5 * time.Minute, EventRetention: time.Hour, EventBatchSize: 100}, clock.Real{}, uuidx.DefaultGenerator{}, txManager, clientRepo, authRepo, authService)
	if err != nil {
		t.Fatalf("new client service: %v", err)
	}

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)), authService, clientService)
	mux := http.NewServeMux()
	handler.Register(mux)
	binder := appserver.NewHTTPConnectionBinder(handler)
	server := httptest.NewUnstartedServer(mux)
	server.EnableHTTP2 = true
	server.Config.ConnContext = binder.ConnContext
	server.Config.ConnState = binder.ConnState
	server.StartTLS()
	defer server.Close()

	user1Client := newScenarioClient(server)
	user2Client := newScenarioClient(server)

	user1Pub, user1Priv, _ := ed25519.GenerateKey(rand.Reader)
	user2Pub, user2Priv, _ := ed25519.GenerateKey(rand.Reader)
	authenticateHTTP2Client(t, user1Client, server.URL, user1Pub, user1Priv)
	authenticateHTTP2Client(t, user2Client, server.URL, user2Pub, user2Priv)

	updateResp := callRPC(t, user1Client, server.URL, user1Priv, "updateProfile", map[string]any{"displayName": "Alice", "bio": "hello"})
	if updateResp[0].Parameters["updatedAt"] == nil {
		t.Fatalf("expected updatedAt: %#v", updateResp[0].Parameters)
	}
	updateUser2Resp := callRPC(t, user2Client, server.URL, user2Priv, "updateProfile", map[string]any{"displayName": "Bob"})
	if updateUser2Resp[0].Parameters["updatedAt"] == nil {
		t.Fatalf("expected updatedAt for user2: %#v", updateUser2Resp[0].Parameters)
	}

	sendReqResp := callRPC(t, user1Client, server.URL, user1Priv, "sendFriendRequest", map[string]any{"receiverPublicKey": user2Pub})
	requestID := mustExtractUUIDParam(t, sendReqResp[0].Parameters, "requestId")

	listReqResp := callRPC(t, user2Client, server.URL, user2Priv, "listFriendRequests", map[string]any{"direction": "incoming"})
	items, ok := listReqResp[0].Parameters["items"].([]any)
	if !ok || len(items) == 0 {
		t.Fatalf("expected incoming requests: %#v", listReqResp[0].Parameters)
	}

	acceptResp := callRPC(t, user2Client, server.URL, user2Priv, "acceptFriendRequest", map[string]any{"requestId": requestID})
	if acceptResp[0].Parameters["friendId"] == nil {
		t.Fatalf("expected friendId: %#v", acceptResp[0].Parameters)
	}

	uploadKeyPackagesResp := callRPC(t, user2Client, server.URL, user2Priv, "uploadKeyPackages", map[string]any{
		"packages": []any{
			map[string]any{
				"keyPackageBytes": []byte("key-package"),
				"isLastResort":    false,
				"expiresAt":       time.Now().Add(time.Hour).UTC().UnixMicro(),
			},
		},
	})
	if uploadKeyPackagesResp[0].Parameters["recordedCount"] == nil {
		t.Fatalf("expected recordedCount: %#v", uploadKeyPackagesResp[0].Parameters)
	}

	directRoomResp := callRPC(t, user1Client, server.URL, user1Priv, "createDirectRoom", map[string]any{"targetUserPublicKey": user2Pub})
	directRoomID := mustExtractUUIDParam(t, directRoomResp[0].Parameters, "roomId")
	if directRoomResp[0].Parameters["alreadyExisted"] != false {
		t.Fatalf("expected new direct room: %#v", directRoomResp[0].Parameters)
	}
	directRoomAgainResp := callRPC(t, user2Client, server.URL, user2Priv, "createDirectRoom", map[string]any{"targetUserPublicKey": user1Pub})
	if mustExtractUUIDParam(t, directRoomAgainResp[0].Parameters, "roomId") != directRoomID || directRoomAgainResp[0].Parameters["alreadyExisted"] != true {
		t.Fatalf("expected existing direct room: %#v", directRoomAgainResp[0].Parameters)
	}

	roomResp := callRPC(t, user1Client, server.URL, user1Priv, "createChatRoom", map[string]any{"title": "Room 1", "description": "desc", "visibility": uint64(1)})
	roomID := mustExtractUUIDParam(t, roomResp[0].Parameters, "roomId")

	inviteResp := callRPC(t, user1Client, server.URL, user1Priv, "sendChatInvitation", map[string]any{"roomId": roomID, "inviteePublicKey": user2Pub})
	invitationID := mustExtractUUIDParam(t, inviteResp[0].Parameters, "invitationId")

	acceptInviteResp := callRPC(t, user2Client, server.URL, user2Priv, "acceptChatInvitation", map[string]any{"invitationId": invitationID})
	if acceptInviteResp[0].Parameters["roomId"] == nil {
		t.Fatalf("expected roomId on accept: %#v", acceptInviteResp[0].Parameters)
	}

	msgResp := callRPC(t, user1Client, server.URL, user1Priv, "sendMessage", map[string]any{"roomId": roomID, "clientMsgId": uuid.New(), "body": []any{[]byte("hello")}})
	if msgResp[0].Parameters["messageId"] == nil {
		t.Fatalf("expected messageId: %#v", msgResp[0].Parameters)
	}

	serverConfigResp := callRPC(t, user2Client, server.URL, user2Priv, "getServerConfig", map[string]any{})
	if serverConfigResp[0].Parameters["config"] == nil {
		t.Fatalf("expected config: %#v", serverConfigResp[0].Parameters)
	}

	groupLimitsResp := callRPC(t, user2Client, server.URL, user2Priv, "getGroupLimits", map[string]any{"roomId": roomID})
	if groupLimitsResp[0].Parameters["spent"] == nil {
		t.Fatalf("expected spent: %#v", groupLimitsResp[0].Parameters)
	}

	for i := 0; i < 3; i++ {
		privateRoomResp := callRPC(t, user1Client, server.URL, user1Priv, "createChatRoom", map[string]any{"title": "Paged Room", "description": "desc", "visibility": uint64(1)})
		if privateRoomResp[0].Parameters["roomId"] == nil {
			t.Fatalf("expected roomId in paged room creation: %#v", privateRoomResp[0].Parameters)
		}
	}

	page1 := callRPC(t, user1Client, server.URL, user1Priv, "listChatRooms", map[string]any{"limit": uint64(2)})
	if page1[0].Parameters["nextCursor"] == nil {
		t.Fatalf("expected nextCursor on first rooms page: %#v", page1[0].Parameters)
	}
	page1Items, ok := page1[0].Parameters["items"].([]any)
	if !ok || len(page1Items) != 2 {
		t.Fatalf("expected 2 items on first rooms page: %#v", page1[0].Parameters)
	}
	page2 := callRPC(t, user1Client, server.URL, user1Priv, "listChatRooms", map[string]any{"limit": uint64(2), "cursor": page1[0].Parameters["nextCursor"]})
	page2Items, ok := page2[0].Parameters["items"].([]any)
	if !ok || len(page2Items) == 0 {
		t.Fatalf("expected second rooms page: %#v", page2[0].Parameters)
	}
}

func authenticateHTTP2Client(t *testing.T, client *http.Client, url string, publicKey []byte, privateKey ed25519.PrivateKey) {
	t.Helper()
	challengeResp := callRPC(t, client, url, privateKey, "requestAuthChallenge", map[string]any{"userPublicKey": publicKey, "publicIp": "127.0.0.1", "deviceId": "device-1", "clientNonce": []byte("nonce")})
	sessionID := mustExtractUUIDParam(t, challengeResp[0].Parameters, "sessionId")
	challengePayload, ok := challengeResp[0].Parameters["challengePayload"].([]byte)
	if !ok {
		t.Fatalf("missing challenge payload: %#v", challengeResp[0].Parameters)
	}
	solveResp := callRPC(t, client, url, privateKey, "solveAuthChallenge", map[string]any{"sessionId": sessionID, "signature": ed25519.Sign(privateKey, challengePayload)})
	if solveResp[0].Parameters["isAuthenticated"] != true {
		t.Fatalf("failed auth: %#v", solveResp[0].Parameters)
	}
}

func callRPC(t *testing.T, client *http.Client, url string, privateKey ed25519.PrivateKey, rpcName string, params map[string]any) []rpc.ResponsePacket {
	t.Helper()
	request := rpc.RequestPayload{RequestID: uuid.New(), RPCCall: rpcName, Timestamp: time.Now().UnixMilli(), Version: 2, Parameters: mustCBOR(t, params)}
	packet := mustSignedPacket(t, privateKey, request)
	return postPackets(t, client, url, []rpc.RequestPacket{packet})
}

func mustParseUUIDString(value string) uuid.UUID {
	parsed, _ := uuid.Parse(value)
	return parsed
}

func newScenarioClient(server *httptest.Server) *http.Client {
	base := server.Client()
	transport := base.Transport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	return &http.Client{Transport: transport}
}

func mustExtractUUIDParam(t *testing.T, params map[string]any, key string) uuid.UUID {
	t.Helper()
	value, ok := params[key]
	if !ok || value == nil {
		t.Fatalf("missing %s: %#v", key, params)
	}
	switch typed := value.(type) {
	case string:
		return mustParseUUIDString(typed)
	case uuid.UUID:
		return typed
	case []byte:
		parsed, err := uuid.FromBytes(typed)
		if err != nil {
			t.Fatalf("invalid uuid bytes for %s: %v", key, err)
		}
		return parsed
	default:
		t.Fatalf("unexpected uuid type for %s: %T (%#v)", key, value, value)
		return uuid.Nil
	}
}
