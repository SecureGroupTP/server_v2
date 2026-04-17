package clientrpc

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	appauth "server_v2/internal/application/auth"
	clientapi "server_v2/internal/application/clientapi"
	appoutbox "server_v2/internal/application/outbox"
	domainauth "server_v2/internal/domain/auth"
	"server_v2/internal/domain/rpc"
	"server_v2/internal/platform/eventbus"
	"server_v2/internal/platform/logging"
	appserver "server_v2/internal/server"
)

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(_ *http.Request) bool {
		return true
	},
}

const eventRedeliveryPollInterval = 1 * time.Second

type Handler struct {
	logger           *slog.Logger
	authService      *appauth.Service
	clientService    *clientapi.Service
	outboxService    *appoutbox.Service
	httpConnSessions *httpConnectionSessions
	bus              *eventbus.Bus
}

type sessionState struct {
	SessionID        uuid.UUID
	UserPublicKey    []byte
	DeviceID         string
	Authenticated    bool
	ProfileCompleted bool
	SubscriptionIDs  []uuid.UUID
}

type connectionSession struct {
	mu    sync.Mutex
	state sessionState
	sent  map[uuid.UUID]time.Time
}

type httpConnectionSessions struct {
	mu     sync.RWMutex
	scopes map[string]*connectionSession
}

type requestAuthChallengeParams struct {
	UserPublicKey []byte `cbor:"userPublicKey"`
	PublicIP      string `cbor:"publicIp"`
	DeviceID      string `cbor:"deviceId"`
	ClientNonce   []byte `cbor:"clientNonce"`
}

type solveAuthChallengeParams struct {
	SessionID uuid.UUID `cbor:"sessionId"`
	Signature []byte    `cbor:"signature"`
}

type subscribeToEventsParams struct {
	RequestedAt int64 `cbor:"requestedAt"`
}

type unsubscribeFromEventsParams struct {
	SubscriptionID uuid.UUID `cbor:"subscriptionId"`
	RequestedAt    int64     `cbor:"requestedAt"`
}

type acknowledgeEventParams struct {
	EventID   any    `cbor:"eventId"`
	DeviceID  string `cbor:"deviceId"`
	SegmentID string `cbor:"segmentId"`
}

func NewHandler(logger *slog.Logger, authService *appauth.Service, clientService *clientapi.Service, outboxService *appoutbox.Service, bus *eventbus.Bus) *Handler {
	return &Handler{
		logger:           logging.WithSource(logger, "server_v2/internal/delivery/clientrpc.Handler"),
		authService:      authService,
		clientService:    clientService,
		outboxService:    outboxService,
		httpConnSessions: newHTTPConnectionSessions(),
		bus:              bus,
	}
}

func newHTTPConnectionSessions() *httpConnectionSessions {
	return &httpConnectionSessions{
		scopes: make(map[string]*connectionSession),
	}
}

func (s *httpConnectionSessions) get(connectionID string) *connectionSession {
	s.mu.RLock()
	scope, ok := s.scopes[connectionID]
	s.mu.RUnlock()
	if ok {
		return scope
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	scope, ok = s.scopes[connectionID]
	if ok {
		return scope
	}

	scope = &connectionSession{sent: make(map[uuid.UUID]time.Time)}
	s.scopes[connectionID] = scope
	return scope
}

func (s *httpConnectionSessions) delete(connectionID string) {
	s.mu.Lock()
	delete(s.scopes, connectionID)
	s.mu.Unlock()
}

func (h *Handler) OnHTTPConnectionClosed(connectionID string) {
	if strings.TrimSpace(connectionID) == "" {
		return
	}
	h.httpConnSessions.delete(connectionID)
	h.logger.Debug("http connection session released", "connection_id", connectionID)
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/client", h.handleHTTP)
}

func (h *Handler) handleHTTP(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()
	if websocket.IsWebSocketUpgrade(r) {
		h.handleWebSocket(w, r)
		return
	}
	if r.Method != http.MethodPost {
		h.logger.Warn("rpc http request rejected", "method", r.Method, "path", r.URL.Path, "reason", "method_not_allowed")
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	defer func() {
		_ = r.Body.Close()
	}()
	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		h.logger.Warn("failed to read rpc http body", "path", r.URL.Path, "error", err)
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	connectionID, ok := appserver.HTTPConnectionIDFromContext(r.Context())
	if !ok {
		h.logger.Error("missing http connection context", "path", r.URL.Path)
		http.Error(w, "missing http connection context", http.StatusInternalServerError)
		return
	}

	scope := h.httpConnSessions.get(connectionID)
	scope.mu.Lock()
	responses, nextState, statusCode, requestCount := h.handleBatch(r.Context(), body, scope.state, scope.sent)
	scope.state = nextState
	scope.mu.Unlock()
	payload, err := cbor.Marshal(responses)
	if err != nil {
		h.logger.Error("failed to encode rpc response", "connection_id", connectionID, "response_count", len(responses), "error", err)
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/cbor")
	w.WriteHeader(statusCode)
	_, _ = w.Write(payload)
	h.recordUsage(r.Context(), nextState, requestCount, len(body), len(payload))
	h.logger.Info(
		"rpc http request completed",
		"connection_id", connectionID,
		"request_bytes", len(body),
		"response_bytes", len(payload),
		"response_count", len(responses),
		"status_code", statusCode,
		"authenticated", nextState.Authenticated,
		"profile_completed", nextState.ProfileCompleted,
		"duration_ms", time.Since(startedAt).Milliseconds(),
	)
}

func (h *Handler) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Warn("websocket upgrade failed", "path", r.URL.Path, "remote_addr", r.RemoteAddr, "error", err)
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	var writeMu sync.Mutex
	writePackets := func(packets []rpc.ResponsePacket) (int, error) {
		encoded, err := cbor.Marshal(packets)
		if err != nil {
			return 0, err
		}
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if err := conn.WriteMessage(websocket.BinaryMessage, encoded); err != nil {
			return 0, err
		}
		return len(encoded), nil
	}

	defer func() {
		_ = conn.Close()
		h.logger.Info("websocket connection closed", "remote_addr", r.RemoteAddr, "duration_ms", time.Since(startedAt).Milliseconds())
	}()
	h.logger.Info("websocket connection accepted", "path", r.URL.Path, "remote_addr", r.RemoteAddr)

	state := sessionState{}
	sentEvents := make(map[uuid.UUID]time.Time)
	var sentEventsMu sync.Mutex
	var stopPush func()
	startPush := func(state sessionState) {
		if stopPush != nil || h.bus == nil || state.DeviceID == "" || h.outboxService == nil {
			return
		}
		notifyCh, unsubscribe := h.bus.SubscribeKey(state.DeviceID)
		pushCtx, pushCancel := context.WithCancel(ctx)
		var stopOnce sync.Once
		stop := func() {
			stopOnce.Do(func() {
				pushCancel()
				unsubscribe()
			})
		}
		stopPush = stop

		// Flush any backlog immediately, then wait for new events.
		go func() {
			defer stop()
			for {
				select {
				case <-pushCtx.Done():
					return
				default:
				}

				sentEventsMu.Lock()
				packets, err := h.pullOutboxPackets(pushCtx, state.DeviceID, sentEvents)
				sentEventsMu.Unlock()
				if err != nil {
					h.logger.Debug("websocket event push pull failed", "remote_addr", r.RemoteAddr, "error", err)
					// If we can't pull events, wait for the next signal.
					select {
					case <-notifyCh:
						continue
					case <-time.After(eventRedeliveryPollInterval):
						continue
					case <-pushCtx.Done():
						return
					}
				}
				if len(packets) > 0 {
					bytesOut, err := writePackets(packets)
					if err != nil {
						h.logger.Debug("websocket event push write failed", "remote_addr", r.RemoteAddr, "error", err)
						cancel()
						return
					}
					h.recordUserUsage(pushCtx, state.UserPublicKey, 0, 0, bytesOut)
					// More events may already be queued; keep draining.
					continue
				}

				select {
				case <-notifyCh:
				case <-time.After(eventRedeliveryPollInterval):
				case <-pushCtx.Done():
					return
				}
			}
		}()
	}

	for {
		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			h.logger.Debug("websocket read stopped", "remote_addr", r.RemoteAddr, "error", err)
			if stopPush != nil {
				stopPush()
				stopPush = nil
			}
			return
		}
		if messageType != websocket.BinaryMessage {
			h.logger.Debug("websocket message ignored", "remote_addr", r.RemoteAddr, "message_type", messageType, "reason", "non_binary")
			continue
		}

		sentEventsMu.Lock()
		responses, nextState, _, requestCount := h.handleBatch(ctx, payload, state, sentEvents)
		sentEventsMu.Unlock()
		state = nextState

		// Start async event push after the connection becomes authenticated.
		if state.Authenticated && h.outboxService != nil {
			startPush(state)
		}

		bytesOut, err := writePackets(responses)
		if err != nil {
			h.logger.Warn("failed to write websocket rpc response", "remote_addr", r.RemoteAddr, "response_count", len(responses), "error", err)
			if stopPush != nil {
				stopPush()
				stopPush = nil
			}
			return
		}
		h.recordUsage(ctx, state, requestCount, len(payload), bytesOut)
		h.logger.Info(
			"websocket rpc batch completed",
			"remote_addr", r.RemoteAddr,
			"request_bytes", len(payload),
			"response_count", len(responses),
			"authenticated", state.Authenticated,
			"profile_completed", state.ProfileCompleted,
		)
	}
}

func (h *Handler) HandleStream(ctx context.Context, rw io.ReadWriter) error {
	startedAt := time.Now()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	decoder := cbor.NewDecoder(rw)
	var writeMu sync.Mutex
	writePackets := func(packets []rpc.ResponsePacket) (int, error) {
		encoded, err := cbor.Marshal(packets)
		if err != nil {
			return 0, err
		}
		writeMu.Lock()
		defer writeMu.Unlock()
		written, err := writeAll(rw, encoded)
		if err != nil {
			return 0, err
		}
		return written, nil
	}
	state := sessionState{}
	sentEvents := make(map[uuid.UUID]time.Time)
	var sentEventsMu sync.Mutex
	batches := 0
	h.logger.Info("tcp rpc stream started")
	defer func() {
		h.logger.Info("tcp rpc stream stopped", "batch_count", batches, "duration_ms", time.Since(startedAt).Milliseconds())
	}()

	var stopPush func()
	startPush := func(state sessionState) {
		if stopPush != nil || h.bus == nil || state.DeviceID == "" || h.outboxService == nil {
			return
		}
		notifyCh, unsubscribe := h.bus.SubscribeKey(state.DeviceID)
		pushCtx, pushCancel := context.WithCancel(ctx)
		var stopOnce sync.Once
		stop := func() {
			stopOnce.Do(func() {
				pushCancel()
				unsubscribe()
			})
		}
		stopPush = stop

		go func() {
			defer stop()
			for {
				select {
				case <-pushCtx.Done():
					return
				default:
				}

				sentEventsMu.Lock()
				packets, err := h.pullOutboxPackets(pushCtx, state.DeviceID, sentEvents)
				sentEventsMu.Unlock()
				if err != nil {
					h.logger.Debug("tcp event push pull failed", "error", err)
					select {
					case <-notifyCh:
						continue
					case <-time.After(eventRedeliveryPollInterval):
						continue
					case <-pushCtx.Done():
						return
					}
				}
				if len(packets) > 0 {
					bytesOut, err := writePackets(packets)
					if err != nil {
						h.logger.Debug("tcp event push write failed", "error", err)
						cancel()
						return
					}
					h.recordUserUsage(pushCtx, state.UserPublicKey, 0, 0, bytesOut)
					continue
				}

				select {
				case <-notifyCh:
				case <-time.After(eventRedeliveryPollInterval):
				case <-pushCtx.Done():
					return
				}
			}
		}()
	}

	for {
		var payload cbor.RawMessage
		if err := decoder.Decode(&payload); err != nil {
			if errors.Is(err, io.EOF) {
				if stopPush != nil {
					stopPush()
					stopPush = nil
				}
				return nil
			}
			if stopPush != nil {
				stopPush()
				stopPush = nil
			}
			return fmt.Errorf("decode stream batch: %w", err)
		}

		sentEventsMu.Lock()
		responses, nextState, _, requestCount := h.handleBatch(ctx, payload, state, sentEvents)
		sentEventsMu.Unlock()
		state = nextState

		if state.Authenticated && h.outboxService != nil {
			startPush(state)
		}

		bytesOut, err := writePackets(responses)
		if err != nil {
			h.recordUsage(ctx, state, requestCount, len(payload), 0)
			if stopPush != nil {
				stopPush()
				stopPush = nil
			}
			return fmt.Errorf("encode stream batch: %w", err)
		}
		h.recordUsage(ctx, state, requestCount, len(payload), bytesOut)
		batches++
		h.logger.Info(
			"tcp rpc batch completed",
			"request_count", requestCount,
			"response_count", len(responses),
			"authenticated", state.Authenticated,
			"profile_completed", state.ProfileCompleted,
		)
	}
}

func (h *Handler) handleBatch(ctx context.Context, raw []byte, state sessionState, sentEvents map[uuid.UUID]time.Time) ([]rpc.ResponsePacket, sessionState, int, int) {
	var packets []rpc.RequestPacket
	if err := cbor.Unmarshal(raw, &packets); err != nil {
		var packet rpc.RequestPacket
		if singleErr := cbor.Unmarshal(raw, &packet); singleErr != nil {
			h.logger.Warn("invalid rpc cbor payload rejected", "request_bytes", len(raw), "error", singleErr)
			response := rpc.ResponsePacket{
				RequestID:  uuid.New(),
				EventType:  "rpcCallResponse",
				Parameters: map[string]any{"error": rpc.ErrorBody{Code: "bad_request", Message: "invalid cbor payload"}},
			}
			return []rpc.ResponsePacket{response}, state, http.StatusBadRequest, 0
		}
		packets = []rpc.RequestPacket{packet}
	}

	responses, nextState, statusCode := h.handlePackets(ctx, packets, state, sentEvents)
	return responses, nextState, statusCode, len(packets)
}

func (h *Handler) handlePackets(ctx context.Context, packets []rpc.RequestPacket, state sessionState, sentEvents map[uuid.UUID]time.Time) ([]rpc.ResponsePacket, sessionState, int) {
	startedAt := time.Now()
	responses := make([]rpc.ResponsePacket, 0, len(packets)+4)
	statusCode := http.StatusOK
	currentState := state
	errorCount := 0

	h.logger.Debug("rpc batch started", "request_count", len(packets), "authenticated", state.Authenticated, "profile_completed", state.ProfileCompleted)
	for _, packet := range packets {
		response, nextState, err := h.handlePacket(ctx, packet, currentState)
		currentState = nextState
		if err != nil {
			errorCount++
			statusCode = maxStatus(statusCode, http.StatusBadRequest)
			responses = append(responses, response)
			continue
		}
		responses = append(responses, response)
	}

	if currentState.Authenticated && currentState.DeviceID != "" && h.outboxService != nil {
		events, err := h.pullOutboxPackets(ctx, currentState.DeviceID, sentEvents)
		if err != nil {
			h.logger.Error("failed to pull device events", "session_id", currentState.SessionID.String(), "device_id", currentState.DeviceID, "error", err)
			statusCode = maxStatus(statusCode, http.StatusInternalServerError)
			return responses, currentState, statusCode
		}
		if len(events) > 0 {
			responses = append(responses, events...)
			h.logger.Debug("device events pulled", "session_id", currentState.SessionID.String(), "device_id", currentState.DeviceID, "event_count", len(events))
		}
	} else if currentState.Authenticated && h.authService != nil {
		events, err := h.authService.PullEvents(ctx, appauth.PullEventsInput{UserPublicKey: currentState.UserPublicKey})
		if err != nil {
			h.logger.Error("failed to pull user events", "session_id", currentState.SessionID.String(), "error", err)
			statusCode = maxStatus(statusCode, http.StatusInternalServerError)
			return responses, currentState, statusCode
		}
		for _, event := range events {
			eventID := event.EventID
			responses = append(responses, rpc.ResponsePacket{
				RequestID:        eventID,
				ReplyToRequestID: event.ReplyToRequestID,
				EventType:        event.EventType,
				Parameters:       event.Payload,
			})
		}
		h.logger.Debug("user events pulled", "session_id", currentState.SessionID.String(), "event_count", len(events))
	}

	h.logger.Debug(
		"rpc batch completed",
		"request_count", len(packets),
		"response_count", len(responses),
		"error_count", errorCount,
		"status_code", statusCode,
		"authenticated", currentState.Authenticated,
		"profile_completed", currentState.ProfileCompleted,
		"duration_ms", time.Since(startedAt).Milliseconds(),
	)
	return responses, currentState, statusCode
}

func (h *Handler) pullOutboxPackets(ctx context.Context, deviceID string, sentEvents map[uuid.UUID]time.Time) ([]rpc.ResponsePacket, error) {
	if h.outboxService == nil || deviceID == "" {
		return nil, nil
	}
	if sentEvents == nil {
		sentEvents = make(map[uuid.UUID]time.Time)
	}
	now := time.Now()
	for eventID, expiresAt := range sentEvents {
		if !expiresAt.After(now) {
			delete(sentEvents, eventID)
		}
	}
	events, err := h.outboxService.ListInflight(ctx, deviceID, 128)
	if err != nil {
		return nil, err
	}
	if len(events) == 0 {
		if _, err := h.outboxService.DispatchOnce(ctx); err != nil {
			return nil, err
		}
		events, err = h.outboxService.ListInflight(ctx, deviceID, 128)
		if err != nil {
			return nil, err
		}
	}
	if len(events) == 0 {
		return nil, nil
	}
	packets := make([]rpc.ResponsePacket, 0, len(events))
	for _, event := range events {
		if until, seen := sentEvents[event.EventID]; seen && until.After(now) {
			continue
		}
		eventID := event.EventID
		packets = append(packets, rpc.ResponsePacket{
			RequestID:  eventID,
			EventType:  event.EventType,
			Parameters: event.Payload,
		})
		if event.InflightUntil != nil {
			sentEvents[event.EventID] = *event.InflightUntil
		}
	}
	return packets, nil
}

func (h *Handler) handlePacket(ctx context.Context, packet rpc.RequestPacket, state sessionState) (rpc.ResponsePacket, sessionState, error) {
	startedAt := time.Now()
	payload, err := rpc.DecodePayload(packet.Payload)
	if err != nil {
		h.logger.Warn("rpc packet rejected", "reason", "invalid_payload", "error", err)
		return rpc.ResponsePacket{RequestID: uuid.New(), EventType: "rpcCallResponse", Parameters: errorParameters("bad_request", "invalid payload")}, state, err
	}
	h.logger.Debug("rpc call started", "rpc_call", payload.RPCCall, "request_id", payload.RequestID.String(), "authenticated", state.Authenticated, "profile_completed", state.ProfileCompleted)

	nextState := state
	signer, err := h.resolveSigner(ctx, payload, state)
	if err != nil {
		h.logRPCFailure(payload, err, startedAt)
		return h.errorResponse(payload.RequestID, err), state, err
	}
	if len(signer) > 0 && !ed25519.Verify(signer, packet.Payload, packet.Signature) {
		h.logRPCFailure(payload, domainauth.ErrInvalidSignature, startedAt)
		return h.errorResponse(payload.RequestID, domainauth.ErrInvalidSignature), state, domainauth.ErrInvalidSignature
	}

	switch payload.RPCCall {
	case "requestAuthChallenge":
		var params requestAuthChallengeParams
		if err := rpc.DecodeParameters(payload.Parameters, &params); err != nil {
			h.logRPCFailure(payload, err, startedAt)
			return h.errorResponse(payload.RequestID, err), state, err
		}
		result, err := h.authService.RequestAuthChallenge(ctx, appauth.RequestAuthChallengeInput{
			UserPublicKey: params.UserPublicKey,
			PublicIP:      params.PublicIP,
			DeviceID:      params.DeviceID,
			ClientNonce:   params.ClientNonce,
		})
		if err != nil {
			h.logRPCFailure(payload, err, startedAt)
			return h.errorResponse(payload.RequestID, err), state, err
		}
		nextState.SessionID = result.SessionID
		nextState.UserPublicKey = append([]byte(nil), params.UserPublicKey...)
		nextState.DeviceID = params.DeviceID
		h.logRPCSuccess(payload, nextState, startedAt)
		return rpc.ResponsePacket{
			RequestID:        uuid.New(),
			ReplyToRequestID: &payload.RequestID,
			EventType:        "rpcCallResponse",
			Parameters: map[string]any{
				"sessionId":        result.SessionID.String(),
				"challengePayload": result.ChallengePayload,
				"expiresAt":        result.ExpiresAt.UTC().Format(time.RFC3339Nano),
			},
		}, nextState, nil
	case "solveAuthChallenge":
		var params solveAuthChallengeParams
		if err := rpc.DecodeParameters(payload.Parameters, &params); err != nil {
			h.logRPCFailure(payload, err, startedAt)
			return h.errorResponse(payload.RequestID, err), state, err
		}
		result, err := h.authService.SolveAuthChallenge(ctx, appauth.SolveAuthChallengeInput{
			SessionID: params.SessionID,
			Signature: params.Signature,
		})
		if err != nil {
			h.logRPCFailure(payload, err, startedAt)
			return h.errorResponse(payload.RequestID, err), state, err
		}
		nextState.SessionID = params.SessionID
		nextState.UserPublicKey = append([]byte(nil), result.UserPublicKey...)
		nextState.Authenticated = result.IsAuthenticated
		if result.IsAuthenticated {
			session, err := h.authService.LookupSession(ctx, params.SessionID)
			if err != nil {
				h.logRPCFailure(payload, err, startedAt)
				return h.errorResponse(payload.RequestID, err), state, err
			}
			nextState.DeviceID = session.DeviceID
			profileCompleted, err := h.profileCompleted(ctx, result.UserPublicKey)
			if err != nil {
				h.logRPCFailure(payload, err, startedAt)
				return h.errorResponse(payload.RequestID, err), state, err
			}
			nextState.ProfileCompleted = profileCompleted
		}
		h.logRPCSuccess(payload, nextState, startedAt)
		return rpc.ResponsePacket{
			RequestID:        uuid.New(),
			ReplyToRequestID: &payload.RequestID,
			EventType:        "rpcCallResponse",
			Parameters: map[string]any{
				"isAuthenticated": result.IsAuthenticated,
				"userPublicKey":   result.UserPublicKey,
				"serverTime":      result.ServerTime.UTC().Format(time.RFC3339Nano),
			},
		}, nextState, nil
	case "subscribeToEvents":
		var params subscribeToEventsParams
		if err := rpc.DecodeParameters(payload.Parameters, &params); err != nil {
			h.logRPCFailure(payload, err, startedAt)
			return h.errorResponse(payload.RequestID, err), state, err
		}
		_ = params
		result, err := h.authService.SubscribeToEvents(ctx, appauth.SubscribeToEventsInput{SessionID: nextState.SessionID})
		if err != nil {
			h.logRPCFailure(payload, err, startedAt)
			return h.errorResponse(payload.RequestID, err), state, err
		}
		nextState.SubscriptionIDs = append(nextState.SubscriptionIDs, result.SubscriptionID)
		h.logRPCSuccess(payload, nextState, startedAt)
		return rpc.ResponsePacket{
			RequestID:        uuid.New(),
			ReplyToRequestID: &payload.RequestID,
			EventType:        "rpcCallResponse",
			Parameters: map[string]any{
				"subscriptionId": result.SubscriptionID.String(),
				"subscribedAt":   result.SubscribedAt.UTC().Format(time.RFC3339Nano),
			},
		}, nextState, nil
	case "unsubscribeFromEvents":
		var params unsubscribeFromEventsParams
		if err := rpc.DecodeParameters(payload.Parameters, &params); err != nil {
			h.logRPCFailure(payload, err, startedAt)
			return h.errorResponse(payload.RequestID, err), state, err
		}
		result, err := h.authService.UnsubscribeFromEvents(ctx, appauth.UnsubscribeFromEventsInput{SubscriptionID: params.SubscriptionID})
		if err != nil {
			h.logRPCFailure(payload, err, startedAt)
			return h.errorResponse(payload.RequestID, err), state, err
		}
		h.logRPCSuccess(payload, nextState, startedAt)
		return rpc.ResponsePacket{
			RequestID:        uuid.New(),
			ReplyToRequestID: &payload.RequestID,
			EventType:        "rpcCallResponse",
			Parameters: map[string]any{
				"unsubscribedAt": result.UnsubscribedAt.UTC().Format(time.RFC3339Nano),
			},
		}, nextState, nil
	case "acknowledgeEvent":
		if !nextState.Authenticated {
			h.logRPCFailure(payload, domainauth.ErrSessionNotAuthenticated, startedAt)
			return h.errorResponse(payload.RequestID, domainauth.ErrSessionNotAuthenticated), state, domainauth.ErrSessionNotAuthenticated
		}
		var params acknowledgeEventParams
		if err := rpc.DecodeParameters(payload.Parameters, &params); err != nil {
			h.logRPCFailure(payload, err, startedAt)
			return h.errorResponse(payload.RequestID, err), state, err
		}
		eventID, err := parseUUIDParam(params.EventID)
		if err != nil {
			h.logRPCFailure(payload, domainauth.ErrInvalidEventID, startedAt)
			return h.errorResponse(payload.RequestID, domainauth.ErrInvalidEventID), state, domainauth.ErrInvalidEventID
		}
		deviceID := strings.TrimSpace(params.DeviceID)
		if deviceID == "" {
			deviceID = nextState.DeviceID
		}
		if h.outboxService != nil {
			if err := h.outboxService.Acknowledge(ctx, eventID, deviceID, params.SegmentID); err != nil {
				h.logRPCFailure(payload, err, startedAt)
				return h.errorResponse(payload.RequestID, err), state, err
			}
		} else {
			if _, err := h.authService.AcknowledgeEvent(ctx, appauth.AcknowledgeEventInput{
				UserPublicKey: nextState.UserPublicKey,
				EventID:       eventID,
			}); err != nil {
				h.logRPCFailure(payload, err, startedAt)
				return h.errorResponse(payload.RequestID, err), state, err
			}
		}
		h.logRPCSuccess(payload, nextState, startedAt)
		return rpc.ResponsePacket{
			RequestID:        uuid.New(),
			ReplyToRequestID: &payload.RequestID,
			EventType:        "rpcCallResponse",
			Parameters:       map[string]any{},
		}, nextState, nil
	default:
		response, responseState, handledErr := h.handleClientAPICall(ctx, payload, nextState)
		if handledErr == nil {
			h.logRPCSuccess(payload, responseState, startedAt)
			return response, responseState, nil
		}
		h.logRPCFailure(payload, handledErr, startedAt)
		return h.errorResponse(payload.RequestID, handledErr), state, handledErr
	}
}

func (h *Handler) profileCompleted(ctx context.Context, userPublicKey []byte) (bool, error) {
	if h.clientService == nil {
		return true, nil
	}
	response, err := h.clientService.GetProfile(ctx, userPublicKey)
	if err != nil {
		if errors.Is(err, clientapi.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	profile, ok := response["profile"].(map[string]any)
	if !ok {
		return false, nil
	}
	displayName, _ := profile["displayName"].(string)
	return strings.TrimSpace(displayName) != "", nil
}

func (h *Handler) logRPCSuccess(payload rpc.RequestPayload, state sessionState, startedAt time.Time) {
	h.logger.Debug(
		"rpc call completed",
		"rpc_call", payload.RPCCall,
		"request_id", payload.RequestID.String(),
		"session_id", state.SessionID.String(),
		"authenticated", state.Authenticated,
		"profile_completed", state.ProfileCompleted,
		"duration_ms", time.Since(startedAt).Milliseconds(),
	)
}

func (h *Handler) logRPCFailure(payload rpc.RequestPayload, err error, startedAt time.Time) {
	code, _ := mapError(err)
	h.logger.Warn(
		"rpc call failed",
		"rpc_call", payload.RPCCall,
		"request_id", payload.RequestID.String(),
		"error_code", code,
		"duration_ms", time.Since(startedAt).Milliseconds(),
		"error", err,
	)
}

func (h *Handler) resolveSigner(ctx context.Context, payload rpc.RequestPayload, state sessionState) ([]byte, error) {
	if state.Authenticated {
		return state.UserPublicKey, nil
	}

	switch payload.RPCCall {
	case "requestAuthChallenge":
		var params requestAuthChallengeParams
		if err := rpc.DecodeParameters(payload.Parameters, &params); err != nil {
			return nil, err
		}
		return params.UserPublicKey, nil
	case "solveAuthChallenge":
		var params solveAuthChallengeParams
		if err := rpc.DecodeParameters(payload.Parameters, &params); err != nil {
			return nil, err
		}
		session, err := h.authService.LookupSession(ctx, params.SessionID)
		if err != nil {
			return nil, err
		}
		return session.UserPublicKey, nil
	default:
		if state.SessionID != uuid.Nil {
			return state.UserPublicKey, nil
		}
		return nil, domainauth.ErrSessionNotAuthenticated
	}
}

func (h *Handler) errorResponse(requestID uuid.UUID, err error) rpc.ResponsePacket {
	code, message := mapError(err)
	return rpc.ResponsePacket{
		RequestID:        uuid.New(),
		ReplyToRequestID: &requestID,
		EventType:        "rpcCallResponse",
		Parameters:       errorParameters(code, message),
	}
}

func errorParameters(code string, message string) map[string]any {
	return map[string]any{
		"error": map[string]any{
			"code":    code,
			"message": message,
			"retry":   code == "internal_error",
		},
	}
}

func mapError(err error) (string, string) {
	switch {
	case err == nil:
		return "", ""
	case strings.Contains(err.Error(), "not implemented"):
		return "not_implemented", err.Error()
	case errors.Is(err, domainauth.ErrInvalidPublicKey),
		errors.Is(err, domainauth.ErrInvalidSessionID),
		errors.Is(err, domainauth.ErrInvalidSignature),
		errors.Is(err, domainauth.ErrInvalidDeviceID),
		errors.Is(err, appoutbox.ErrInvalidDeviceID),
		errors.Is(err, domainauth.ErrInvalidClientNonce),
		errors.Is(err, domainauth.ErrInvalidEventID):
		return "invalid_argument", err.Error()
	case errors.Is(err, domainauth.ErrSessionNotFound),
		errors.Is(err, domainauth.ErrSubscriptionNotFound),
		errors.Is(err, clientapi.ErrNotFound):
		return "not_found", err.Error()
	case errors.Is(err, clientapi.ErrForbidden):
		return "forbidden", err.Error()
	case errors.Is(err, clientapi.ErrConflict):
		return "conflict", err.Error()
	case errors.Is(err, clientapi.ErrProfileRequired):
		return "profile_required", err.Error()
	case errors.Is(err, domainauth.ErrSessionExpired),
		errors.Is(err, domainauth.ErrSessionNotAuthenticated):
		return "unauthenticated", err.Error()
	default:
		return "internal_error", err.Error()
	}
}

func maxStatus(current int, candidate int) int {
	if candidate > current {
		return candidate
	}
	return current
}

func (h *Handler) recordUsage(ctx context.Context, state sessionState, requests int, bytesIn int, bytesOut int) {
	if !state.Authenticated {
		return
	}
	h.recordUserUsage(ctx, state.UserPublicKey, requests, bytesIn, bytesOut)
}

func (h *Handler) recordUserUsage(ctx context.Context, userPublicKey []byte, requests int, bytesIn int, bytesOut int) {
	if h.clientService == nil || len(userPublicKey) != ed25519.PublicKeySize {
		return
	}
	if err := h.clientService.RecordUserUsage(ctx, userPublicKey, requests, bytesIn, bytesOut); err != nil {
		h.logger.Debug("failed to record user usage", "error", err)
	}
}

func parseUUIDParam(value any) (uuid.UUID, error) {
	switch v := value.(type) {
	case uuid.UUID:
		if v == uuid.Nil {
			return uuid.Nil, domainauth.ErrInvalidEventID
		}
		return v, nil
	case []byte:
		id, err := uuid.FromBytes(v)
		if err != nil || id == uuid.Nil {
			return uuid.Nil, domainauth.ErrInvalidEventID
		}
		return id, nil
	case string:
		id, err := uuid.Parse(v)
		if err != nil || id == uuid.Nil {
			return uuid.Nil, domainauth.ErrInvalidEventID
		}
		return id, nil
	default:
		return uuid.Nil, domainauth.ErrInvalidEventID
	}
}

func writeAll(w io.Writer, payload []byte) (int, error) {
	total := 0
	for len(payload) > 0 {
		n, err := w.Write(payload)
		total += n
		if err != nil {
			return total, err
		}
		if n <= 0 {
			return total, io.ErrUnexpectedEOF
		}
		payload = payload[n:]
	}
	return total, nil
}
