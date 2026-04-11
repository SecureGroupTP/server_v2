package clientrpcintegration

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	appauth "server_v2/internal/application/auth"
	clientapi "server_v2/internal/application/clientapi"
	"server_v2/internal/config"
	"server_v2/internal/delivery/clientrpc"
	"server_v2/internal/domain/rpc"
	"server_v2/internal/platform/clock"
	"server_v2/internal/platform/logging"
	"server_v2/internal/platform/randombytes"
	"server_v2/internal/platform/uuidx"
	"server_v2/internal/repository/postgres"
	appserver "server_v2/internal/server"
	"server_v2/internal/testutil/postgrestest"
)

const (
	friendRequestPending  = 1
	friendRequestAccepted = 2
	friendRequestDeclined = 3
)

type discoveryPayload struct {
	TCPPort int `cbor:"tcp_port"`
	WSPort  int `cbor:"ws_port"`
}

type rpcClient interface {
	Call(ctx context.Context, rpcName string, params map[string]any) ([]rpc.ResponsePacket, error)
	Close() error
	PublicKey() []byte
	Sign(message []byte) []byte
}

func TestTCPAndWebSocketSocialFlow(t *testing.T) {
	server := newTestServer(t)
	discovery := server.discover(t)
	if discovery.TCPPort == 0 || discovery.WSPort == 0 {
		t.Fatalf("expected tcp/ws ports in discovery: %#v", discovery)
	}

	tcpUser := newTCPRPCClient(t, discovery.TCPPort)
	t.Cleanup(func() { _ = tcpUser.Close() })
	wsUser := newWSRPCClient(t, discovery.WSPort)
	t.Cleanup(func() { _ = wsUser.Close() })

	authenticateAndCompleteProfile(t, tcpUser, "Alice")
	authenticateAndCompleteProfile(t, wsUser, "Bob")
	drainEvents(t, tcpUser)
	drainEvents(t, wsUser)

	firstRequestID := sendFriendRequest(t, tcpUser, wsUser.PublicKey())
	assertFriendRequestEvent(t, drainEvents(t, wsUser), "friend.requestReceived", firstRequestID)

	declineFriendRequest(t, wsUser, firstRequestID)
	assertFriendRequestState(t, wsUser, firstRequestID, friendRequestDeclined)
	assertFriendRequestEvent(t, drainEvents(t, tcpUser), "friend.requestDeclined", firstRequestID)

	secondRequestID := sendFriendRequest(t, wsUser, tcpUser.PublicKey())
	assertFriendRequestEvent(t, drainEvents(t, tcpUser), "friend.requestReceived", secondRequestID)
	acceptFriendRequest(t, tcpUser, secondRequestID)
	assertFriendRequestState(t, tcpUser, secondRequestID, friendRequestAccepted)
	assertFriendRequestEvent(t, drainEvents(t, wsUser), "friend.requestAccepted", secondRequestID)

	callOK(t, tcpUser, "updateProfile", map[string]any{"displayName": "Alice Cooper", "bio": "new bio"})
	profileEvents := drainEvents(t, wsUser)
	event := assertEvent(t, profileEvents, "profile.updated")
	if displayName, _ := event.Parameters["displayName"].(string); displayName != "Alice Cooper" {
		t.Fatalf("expected updated displayName event payload, got %#v", event.Parameters)
	}

	server.assertLogFormat(t)
	server.assertLogged(t, "http request completed")
	server.assertLogged(t, "tcp rpc batch completed")
	server.assertLogged(t, "websocket rpc batch completed")
	server.assertLogged(t, "rpc call completed", "rpc_call", "sendFriendRequest")
}

type testServer struct {
	baseURL string
	logs    *testLogSink
}

type testLogSink struct {
	mu      sync.Mutex
	builder strings.Builder
}

func (s *testLogSink) Write(payload []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.builder.Write(payload)
}

func (s *testLogSink) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.builder.String()
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()

	dsn := postgrestest.DSN(t)
	store, err := postgres.Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
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

	ports := testPorts(t)
	logs := &testLogSink{}
	logger := logging.WithSource(logging.NewLogger(logs, slog.LevelDebug), "integration/clientrpc.social_flow")
	clientHandler := clientrpc.NewHandler(logger, authService, clientService)
	httpBinder := appserver.NewHTTPConnectionBinder(clientHandler)
	httpHandler := appserver.NewHandler(logger, ports, clientHandler)
	cfg := config.AppConfiguration{
		Name:                "server-v2-integration",
		Host:                "127.0.0.1",
		Ports:               ports,
		OutputPorts:         ports,
		SessionChallengeTTL: 5 * time.Minute,
		EventRetention:      time.Hour,
		EventBatchSize:      100,
		TLS:                 config.AppTLSConfiguration{},
	}
	runtime := appserver.NewRuntime(cfg, httpHandler, clientHandler, httpBinder, logger)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- runtime.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := runtime.Shutdown(shutdownCtx); err != nil {
			t.Fatalf("shutdown runtime: %v", err)
		}
		select {
		case err := <-errCh:
			if err != nil {
				t.Fatalf("runtime stopped with error: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("runtime did not stop")
		}
	})

	server := &testServer{
		baseURL: "http://127.0.0.1:" + strconv.Itoa(ports.HTTPPort),
		logs:    logs,
	}
	server.waitForDiscovery(t, errCh)
	return server
}

func (s *testServer) assertLogFormat(t *testing.T) {
	t.Helper()
	events := s.logEvents(t)
	if len(events) == 0 {
		t.Fatal("expected integration logs, got none")
	}
	for index, event := range events {
		if event["@t"] == "" {
			t.Fatalf("log event %d missing @t: %#v", index, event)
		}
		if event["@m"] == "" {
			t.Fatalf("log event %d missing @m: %#v", index, event)
		}
		if event["@i"] == "" {
			t.Fatalf("log event %d missing @i: %#v", index, event)
		}
		if event["SourceContext"] == "" {
			t.Fatalf("log event %d missing SourceContext: %#v", index, event)
		}
		if _, ok := event["time"]; ok {
			t.Fatalf("log event %d uses slog time field instead of @t: %#v", index, event)
		}
		if _, ok := event["msg"]; ok {
			t.Fatalf("log event %d uses slog msg field instead of @m: %#v", index, event)
		}
	}
}

func (s *testServer) assertLogged(t *testing.T, message string, attrs ...string) {
	t.Helper()
	if len(attrs)%2 != 0 {
		t.Fatalf("attrs must be key/value pairs: %#v", attrs)
	}
	for _, event := range s.logEvents(t) {
		if event["@m"] != message {
			continue
		}
		matched := true
		for i := 0; i < len(attrs); i += 2 {
			value, ok := event[attrs[i]].(string)
			if !ok || value != attrs[i+1] {
				matched = false
				break
			}
		}
		if matched {
			return
		}
	}
	t.Fatalf("log event %q with attrs %#v not found in logs:\n%s", message, attrs, s.logs.String())
}

func (s *testServer) logEvents(t *testing.T) []map[string]any {
	t.Helper()
	raw := strings.TrimSpace(s.logs.String())
	if raw == "" {
		return nil
	}
	lines := strings.Split(raw, "\n")
	events := make([]map[string]any, 0, len(lines))
	for index, line := range lines {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("log line %d is not json: %q: %v", index, line, err)
		}
		events = append(events, event)
	}
	return events
}

func (s *testServer) waitForDiscovery(t *testing.T, errCh <-chan error) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-errCh:
			t.Fatalf("runtime failed while starting: %v", err)
		default:
		}

		req, err := http.NewRequest(http.MethodGet, s.baseURL+"/api/v1/discovery/", nil)
		if err != nil {
			t.Fatalf("new discovery request: %v", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("discovery endpoint did not become ready")
}

func (s *testServer) discover(t *testing.T) discoveryPayload {
	t.Helper()

	resp, err := http.Get(s.baseURL + "/api/v1/discovery/")
	if err != nil {
		t.Fatalf("get discovery: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var payload discoveryPayload
	if err := cbor.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode discovery: %v", err)
	}
	return payload
}

func testPorts(t *testing.T) config.AppPortsConfiguration {
	t.Helper()
	listeners := make([]net.Listener, 0, 6)
	for range 6 {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("allocate port: %v", err)
		}
		listeners = append(listeners, listener)
	}
	ports := make([]int, 0, len(listeners))
	for _, listener := range listeners {
		ports = append(ports, listener.Addr().(*net.TCPAddr).Port)
	}
	for _, listener := range listeners {
		_ = listener.Close()
	}
	return config.AppPortsConfiguration{
		TCPPort:    ports[0],
		TCPTLSPort: ports[1],
		HTTPPort:   ports[2],
		HTTPSPort:  ports[3],
		WSPort:     ports[4],
		WSSPort:    ports[5],
	}
}

type clientKeys struct {
	publicKey  ed25519.PublicKey
	privateKey ed25519.PrivateKey
}

func newClientKeys(t *testing.T) clientKeys {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate keypair: %v", err)
	}
	return clientKeys{publicKey: publicKey, privateKey: privateKey}
}

type tcpRPCClient struct {
	keys    clientKeys
	conn    net.Conn
	encoder *cbor.Encoder
	decoder *cbor.Decoder
	mu      sync.Mutex
}

func newTCPRPCClient(t *testing.T, port int) *tcpRPCClient {
	t.Helper()
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 5*time.Second)
	if err != nil {
		t.Fatalf("connect tcp rpc: %v", err)
	}
	return &tcpRPCClient{keys: newClientKeys(t), conn: conn, encoder: cbor.NewEncoder(conn), decoder: cbor.NewDecoder(conn)}
}

func (c *tcpRPCClient) Call(ctx context.Context, rpcName string, params map[string]any) ([]rpc.ResponsePacket, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if deadline, ok := ctx.Deadline(); ok {
		_ = c.conn.SetDeadline(deadline)
		defer func() { _ = c.conn.SetDeadline(time.Time{}) }()
	}
	packet := signedPacket(c.keys.privateKey, rpcName, params)
	if err := c.encoder.Encode([]rpc.RequestPacket{packet}); err != nil {
		return nil, err
	}
	var responses []rpc.ResponsePacket
	if err := c.decoder.Decode(&responses); err != nil {
		return nil, err
	}
	return responses, nil
}

func (c *tcpRPCClient) Close() error { return c.conn.Close() }

func (c *tcpRPCClient) PublicKey() []byte { return append([]byte(nil), c.keys.publicKey...) }

func (c *tcpRPCClient) Sign(message []byte) []byte { return ed25519.Sign(c.keys.privateKey, message) }

type wsRPCClient struct {
	keys clientKeys
	conn *websocket.Conn
	mu   sync.Mutex
}

func newWSRPCClient(t *testing.T, port int) *wsRPCClient {
	t.Helper()
	conn, resp, err := websocket.DefaultDialer.Dial("ws://127.0.0.1:"+strconv.Itoa(port)+"/api/v1/client", nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("connect websocket rpc: %v", err)
	}
	return &wsRPCClient{keys: newClientKeys(t), conn: conn}
}

func (c *wsRPCClient) Call(ctx context.Context, rpcName string, params map[string]any) ([]rpc.ResponsePacket, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	deadline := time.Now().Add(5 * time.Second)
	if ctxDeadline, ok := ctx.Deadline(); ok {
		deadline = ctxDeadline
	}
	packet := signedPacket(c.keys.privateKey, rpcName, params)
	payload, err := cbor.Marshal([]rpc.RequestPacket{packet})
	if err != nil {
		return nil, err
	}
	_ = c.conn.SetWriteDeadline(deadline)
	if err := c.conn.WriteMessage(websocket.BinaryMessage, payload); err != nil {
		return nil, err
	}
	_ = c.conn.SetReadDeadline(deadline)
	messageType, responsePayload, err := c.conn.ReadMessage()
	if err != nil {
		return nil, err
	}
	if messageType != websocket.BinaryMessage {
		return nil, fmt.Errorf("unexpected websocket message type %d", messageType)
	}
	var responses []rpc.ResponsePacket
	if err := cbor.Unmarshal(responsePayload, &responses); err != nil {
		return nil, err
	}
	return responses, nil
}

func (c *wsRPCClient) Close() error { return c.conn.Close() }

func (c *wsRPCClient) PublicKey() []byte { return append([]byte(nil), c.keys.publicKey...) }

func (c *wsRPCClient) Sign(message []byte) []byte { return ed25519.Sign(c.keys.privateKey, message) }

func signedPacket(privateKey ed25519.PrivateKey, rpcName string, params map[string]any) rpc.RequestPacket {
	payload := rpc.RequestPayload{RequestID: uuid.New(), RPCCall: rpcName, Timestamp: time.Now().UnixMilli(), Version: 2, Parameters: mustCBOR(params)}
	rawPayload := mustCBOR(payload)
	return rpc.RequestPacket{Signature: ed25519.Sign(privateKey, rawPayload), Payload: rawPayload}
}

func mustCBOR(value any) []byte {
	payload, err := cbor.Marshal(value)
	if err != nil {
		panic(err)
	}
	return payload
}

func authenticateAndCompleteProfile(t *testing.T, client rpcClient, displayName string) {
	t.Helper()

	challenge := callOK(t, client, "requestAuthChallenge", map[string]any{"userPublicKey": client.PublicKey(), "publicIp": "127.0.0.1", "deviceId": displayName + "-device", "clientNonce": []byte(displayName + "-nonce")})
	sessionID := mustUUIDParam(t, challenge[0].Parameters, "sessionId")
	challengePayload, ok := challenge[0].Parameters["challengePayload"].([]byte)
	if !ok {
		t.Fatalf("missing challenge payload: %#v", challenge[0].Parameters)
	}

	solve := callOK(t, client, "solveAuthChallenge", map[string]any{"sessionId": sessionID, "signature": client.Sign(challengePayload)})
	if solve[0].Parameters["isAuthenticated"] != true {
		t.Fatalf("auth failed: %#v", solve[0].Parameters)
	}

	callOK(t, client, "updateProfile", map[string]any{"displayName": displayName})
}

func sendFriendRequest(t *testing.T, sender rpcClient, receiverPublicKey []byte) uuid.UUID {
	t.Helper()
	response := callOK(t, sender, "sendFriendRequest", map[string]any{"receiverPublicKey": receiverPublicKey})
	requestID := mustUUIDParam(t, response[0].Parameters, "requestId")
	if state := intParam(response[0].Parameters, "state"); state != friendRequestPending {
		t.Fatalf("expected pending request state, got %d: %#v", state, response[0].Parameters)
	}
	return requestID
}

func declineFriendRequest(t *testing.T, client rpcClient, requestID uuid.UUID) {
	t.Helper()
	response := callOK(t, client, "declineFriendRequest", map[string]any{"requestId": requestID})
	if mustUUIDParam(t, response[0].Parameters, "requestId") != requestID {
		t.Fatalf("unexpected decline response: %#v", response[0].Parameters)
	}
}

func acceptFriendRequest(t *testing.T, client rpcClient, requestID uuid.UUID) {
	t.Helper()
	response := callOK(t, client, "acceptFriendRequest", map[string]any{"requestId": requestID})
	if response[0].Parameters["friendId"] == nil {
		t.Fatalf("missing friendId: %#v", response[0].Parameters)
	}
}

func assertFriendRequestState(t *testing.T, client rpcClient, requestID uuid.UUID, expectedState int) {
	t.Helper()
	response := callOK(t, client, "listFriendRequests", map[string]any{"limit": uint64(20)})
	for _, item := range anySlice(t, response[0].Parameters["items"]) {
		mapped := anyMap(t, item)
		if mustUUIDParam(t, mapped, "requestId") == requestID {
			if state := intParam(mapped, "state"); state != expectedState {
				t.Fatalf("request %s state = %d, want %d", requestID, state, expectedState)
			}
			return
		}
	}
	t.Fatalf("request %s not found in listFriendRequests: %#v", requestID, response[0].Parameters)
}

func assertFriendRequestEvent(t *testing.T, events []rpc.ResponsePacket, eventType string, requestID uuid.UUID) {
	t.Helper()
	event := assertEvent(t, events, eventType)
	if got := mustUUIDParam(t, event.Parameters, "requestId"); got != requestID {
		t.Fatalf("event %s requestId = %s, want %s", eventType, got, requestID)
	}
}

func drainEvents(t *testing.T, client rpcClient) []rpc.ResponsePacket {
	t.Helper()
	responses := callOK(t, client, "getServerConfig", map[string]any{})
	return responses[1:]
}

func assertEvent(t *testing.T, events []rpc.ResponsePacket, eventType string) rpc.ResponsePacket {
	t.Helper()
	for _, event := range events {
		if event.EventType == eventType {
			return event
		}
	}
	t.Fatalf("event %s not found in %#v", eventType, eventTypes(events))
	return rpc.ResponsePacket{}
}

func eventTypes(events []rpc.ResponsePacket) []string {
	types := make([]string, 0, len(events))
	for _, event := range events {
		types = append(types, event.EventType)
	}
	return types
}

func callOK(t *testing.T, client rpcClient, rpcName string, params map[string]any) []rpc.ResponsePacket {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	response, err := client.Call(ctx, rpcName, params)
	if err != nil {
		t.Fatalf("%s: %v", rpcName, err)
	}
	if len(response) == 0 {
		t.Fatalf("%s: empty response", rpcName)
	}
	if errorBody, ok := response[0].Parameters["error"]; ok {
		t.Fatalf("%s returned error: %#v", rpcName, errorBody)
	}
	return response
}

func mustUUIDParam(t *testing.T, params map[string]any, key string) uuid.UUID {
	t.Helper()
	value, ok := params[key]
	if !ok || value == nil {
		t.Fatalf("missing %s: %#v", key, params)
	}
	switch typed := value.(type) {
	case uuid.UUID:
		return typed
	case string:
		parsed, err := uuid.Parse(typed)
		if err != nil {
			t.Fatalf("invalid uuid string for %s: %v", key, err)
		}
		return parsed
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

func intParam(params map[string]any, key string) int {
	value := params[key]
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case uint64:
		return int(typed)
	case uint32:
		return int(typed)
	default:
		return 0
	}
}

func anySlice(t *testing.T, value any) []any {
	t.Helper()
	items, ok := value.([]any)
	if !ok {
		t.Fatalf("expected []any, got %T (%#v)", value, value)
	}
	return items
}

func anyMap(t *testing.T, value any) map[string]any {
	t.Helper()
	switch typed := value.(type) {
	case map[string]any:
		return typed
	case map[any]any:
		mapped := make(map[string]any, len(typed))
		for key, value := range typed {
			stringKey, ok := key.(string)
			if !ok {
				t.Fatalf("expected string key, got %T", key)
			}
			mapped[stringKey] = value
		}
		return mapped
	default:
		t.Fatalf("expected map, got %T (%#v)", value, value)
		return nil
	}
}
