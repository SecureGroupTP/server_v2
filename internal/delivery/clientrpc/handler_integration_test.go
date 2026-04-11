package clientrpc

import (
	"bytes"
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

	"github.com/fxamacker/cbor/v2"
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

func TestHandlerAuthHTTPFlow(t *testing.T) {
	client, serverURL := newHandlerTestClient(t)

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate keypair: %v", err)
	}

	requestID := uuid.New()
	challengePayload := rpc.RequestPayload{
		RequestID: requestID,
		RPCCall:   "requestAuthChallenge",
		Timestamp: time.Now().UnixMilli(),
		Version:   2,
		Parameters: mustCBOR(t, map[string]any{
			"userPublicKey": publicKey,
			"publicIp":      "127.0.0.1",
			"deviceId":      "device-1",
			"clientNonce":   []byte("nonce"),
		}),
	}
	challengePacket := mustSignedPacket(t, privateKey, challengePayload)
	challengeResponse := postPackets(t, client, serverURL, []rpc.RequestPacket{challengePacket})
	if len(challengeResponse) != 1 {
		t.Fatalf("expected one challenge response, got %d", len(challengeResponse))
	}
	sessionIDValue, ok := challengeResponse[0].Parameters["sessionId"].(string)
	if !ok || sessionIDValue == "" {
		t.Fatalf("missing sessionId in response: %#v", challengeResponse[0].Parameters)
	}
	sessionID := uuid.MustParse(sessionIDValue)
	challengeBytes, ok := challengeResponse[0].Parameters["challengePayload"].([]byte)
	if !ok {
		t.Fatalf("missing challenge payload bytes: %#v", challengeResponse[0].Parameters)
	}

	solveRequestID := uuid.New()
	solvePayload := rpc.RequestPayload{
		RequestID: solveRequestID,
		RPCCall:   "solveAuthChallenge",
		Timestamp: time.Now().UnixMilli(),
		Version:   2,
		Parameters: mustCBOR(t, map[string]any{
			"sessionId": sessionID,
			"signature": ed25519.Sign(privateKey, challengeBytes),
		}),
	}
	solvePacket := mustSignedPacket(t, privateKey, solvePayload)
	solveResponse := postPackets(t, client, serverURL, []rpc.RequestPacket{solvePacket})
	if len(solveResponse) < 2 {
		t.Fatalf("expected direct response and one server event, got %d packets", len(solveResponse))
	}
	if solveResponse[0].Parameters["isAuthenticated"] != true {
		t.Fatalf("unexpected solve response: %#v", solveResponse[0].Parameters)
	}
	if solveResponse[1].EventType != "auth.sessionAuthenticated" {
		t.Fatalf("unexpected event type: %s", solveResponse[1].EventType)
	}

	blockedResponse := callRPC(t, client, serverURL, privateKey, "getServerConfig", map[string]any{})
	if len(blockedResponse) != 1 {
		t.Fatalf("expected one profile-required response, got %d", len(blockedResponse))
	}
	if code := extractErrorCode(t, blockedResponse[0].Parameters); code != "profile_required" {
		t.Fatalf("unexpected profile-required response: %#v", blockedResponse[0].Parameters)
	}

	updateProfileResponse := callRPC(t, client, serverURL, privateKey, "updateProfile", map[string]any{"displayName": "Alice"})
	if updateProfileResponse[0].Parameters["updatedAt"] == nil {
		t.Fatalf("expected profile update response: %#v", updateProfileResponse[0].Parameters)
	}

	configResponse := callRPC(t, client, serverURL, privateKey, "getServerConfig", map[string]any{})
	if configResponse[0].Parameters["config"] == nil {
		t.Fatalf("expected config after profile update: %#v", configResponse[0].Parameters)
	}

	subscribeRequestID := uuid.New()
	subscribePayload := rpc.RequestPayload{
		RequestID: subscribeRequestID,
		RPCCall:   "subscribeToEvents",
		Timestamp: time.Now().UnixMilli(),
		Version:   2,
		Parameters: mustCBOR(t, map[string]any{
			"requestedAt": time.Now().UTC().Format(time.RFC3339Nano),
		}),
	}
	subscribePacket := mustSignedPacket(t, privateKey, subscribePayload)
	subscribeResponse := postPackets(t, client, serverURL, []rpc.RequestPacket{subscribePacket})
	if len(subscribeResponse) < 2 {
		t.Fatalf("expected subscribe response and server event, got %d packets", len(subscribeResponse))
	}
	if _, ok := subscribeResponse[0].Parameters["subscriptionId"].(string); !ok {
		t.Fatalf("missing subscription id in response: %#v", subscribeResponse[0].Parameters)
	}
	if subscribeResponse[1].EventType != "auth.eventsSubscribed" {
		t.Fatalf("unexpected subscribe event type: %s", subscribeResponse[1].EventType)
	}

	freshTransport := client.Transport.(*http.Transport).Clone()
	freshTransport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	freshTransport.CloseIdleConnections()
	freshClient := &http.Client{Transport: freshTransport}
	unauthenticatedResponse := postPackets(t, freshClient, serverURL, []rpc.RequestPacket{subscribePacket})
	if len(unauthenticatedResponse) != 1 {
		t.Fatalf("expected one unauthenticated response, got %d", len(unauthenticatedResponse))
	}
	if code := extractErrorCode(t, unauthenticatedResponse[0].Parameters); code != "unauthenticated" {
		t.Fatalf("unexpected unauthenticated response: %#v", unauthenticatedResponse[0].Parameters)
	}
}

func newHandlerTestClient(t *testing.T) (*http.Client, string) {
	t.Helper()

	dsn := postgrestest.DSN(t)
	store, err := postgres.Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	postgrestest.AuthTablesOnlyCleanup(t, store)

	repo := postgres.NewAuthRepository(store.DB())
	clientRepo := postgres.NewClientRepository(store.DB())
	txManager := postgres.NewTxManager(store.DB())
	service, err := appauth.NewService(
		appauth.Config{ChallengeTTL: 5 * time.Minute, EventRetention: time.Hour, EventBatchSize: 100},
		clock.Real{},
		uuidx.DefaultGenerator{},
		randombytes.CryptoReader{},
		txManager,
		repo,
		repo,
		repo,
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	clientService, err := clientapi.NewService(
		clientapi.Config{AppName: "server-v2", Version: "2", SessionChallengeTTL: 5 * time.Minute, EventRetention: time.Hour, EventBatchSize: 100},
		clock.Real{},
		uuidx.DefaultGenerator{},
		txManager,
		clientRepo,
		repo,
		service,
	)
	if err != nil {
		t.Fatalf("new client service: %v", err)
	}

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)), service, clientService)
	mux := http.NewServeMux()
	handler.Register(mux)

	binder := appserver.NewHTTPConnectionBinder(handler)
	server := httptest.NewUnstartedServer(mux)
	server.EnableHTTP2 = true
	server.Config.ConnContext = binder.ConnContext
	server.Config.ConnState = binder.ConnState
	server.StartTLS()
	t.Cleanup(server.Close)

	client := server.Client()
	client.Transport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}

	return client, server.URL
}

func mustSignedPacket(t *testing.T, privateKey ed25519.PrivateKey, payload rpc.RequestPayload) rpc.RequestPacket {
	t.Helper()
	rawPayload := mustCBOR(t, payload)
	return rpc.RequestPacket{
		Signature: ed25519.Sign(privateKey, rawPayload),
		Payload:   rawPayload,
	}
}

func mustCBOR(t *testing.T, value any) []byte {
	t.Helper()
	payload, err := cbor.Marshal(value)
	if err != nil {
		t.Fatalf("marshal cbor: %v", err)
	}
	return payload
}

func postPackets(t *testing.T, client *http.Client, url string, packets []rpc.RequestPacket) []rpc.ResponsePacket {
	t.Helper()
	body := mustCBOR(t, packets)
	req, err := http.NewRequest(http.MethodPost, url+"/api/v1/client", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/cbor")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	var packetsOut []rpc.ResponsePacket
	if err := cbor.Unmarshal(responseBody, &packetsOut); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return packetsOut
}

func extractErrorCode(t *testing.T, parameters map[string]any) string {
	t.Helper()

	rawError, ok := parameters["error"]
	if !ok {
		t.Fatalf("missing error body: %#v", parameters)
	}

	switch value := rawError.(type) {
	case map[string]any:
		code, _ := value["code"].(string)
		return code
	case map[any]any:
		code, _ := value["code"].(string)
		return code
	default:
		t.Fatalf("unexpected error body type: %T", rawError)
		return ""
	}
}
