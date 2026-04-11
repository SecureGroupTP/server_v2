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
	domainauth "server_v2/internal/domain/auth"
	"server_v2/internal/domain/rpc"
	appserver "server_v2/internal/server"
)

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(_ *http.Request) bool {
		return true
	},
}

type Handler struct {
	logger           *slog.Logger
	authService      *appauth.Service
	clientService    *clientapi.Service
	httpConnSessions *httpConnectionSessions
}

type sessionState struct {
	SessionID       uuid.UUID
	UserPublicKey   []byte
	Authenticated   bool
	SubscriptionIDs []uuid.UUID
}

type connectionSession struct {
	mu    sync.Mutex
	state sessionState
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
	RequestedAt string `cbor:"requestedAt"`
}

type unsubscribeFromEventsParams struct {
	SubscriptionID uuid.UUID `cbor:"subscriptionId"`
	RequestedAt    string    `cbor:"requestedAt"`
}

func NewHandler(logger *slog.Logger, authService *appauth.Service, clientService *clientapi.Service) *Handler {
	return &Handler{
		logger:           logger,
		authService:      authService,
		clientService:    clientService,
		httpConnSessions: newHTTPConnectionSessions(),
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

	scope = &connectionSession{}
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
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/client", h.handleHTTP)
}

func (h *Handler) handleHTTP(w http.ResponseWriter, r *http.Request) {
	if websocket.IsWebSocketUpgrade(r) {
		h.handleWebSocket(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	defer func() {
		_ = r.Body.Close()
	}()
	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	connectionID, ok := appserver.HTTPConnectionIDFromContext(r.Context())
	if !ok {
		http.Error(w, "missing http connection context", http.StatusInternalServerError)
		return
	}

	scope := h.httpConnSessions.get(connectionID)
	scope.mu.Lock()
	responses, nextState, statusCode := h.handleBatch(r.Context(), body, scope.state)
	scope.state = nextState
	scope.mu.Unlock()
	payload, err := cbor.Marshal(responses)
	if err != nil {
		h.logger.Error("failed to encode rpc response", "error", err)
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/cbor")
	w.WriteHeader(statusCode)
	_, _ = w.Write(payload)
}

func (h *Handler) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Warn("websocket upgrade failed", "error", err)
		return
	}
	defer func() {
		_ = conn.Close()
	}()

	state := sessionState{}
	for {
		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if messageType != websocket.BinaryMessage {
			continue
		}

		responses, nextState, _ := h.handleBatch(r.Context(), payload, state)
		state = nextState
		encoded, err := cbor.Marshal(responses)
		if err != nil {
			h.logger.Error("failed to encode websocket rpc response", "error", err)
			return
		}
		_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if err := conn.WriteMessage(websocket.BinaryMessage, encoded); err != nil {
			return
		}
	}
}

func (h *Handler) HandleStream(ctx context.Context, rw io.ReadWriter) error {
	decoder := cbor.NewDecoder(rw)
	encoder := cbor.NewEncoder(rw)
	state := sessionState{}

	for {
		var payload []rpc.RequestPacket
		if err := decoder.Decode(&payload); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("decode stream batch: %w", err)
		}

		responses, nextState, _ := h.handlePackets(ctx, payload, state)
		state = nextState
		if err := encoder.Encode(responses); err != nil {
			return fmt.Errorf("encode stream batch: %w", err)
		}
	}
}

func (h *Handler) handleBatch(ctx context.Context, raw []byte, state sessionState) ([]rpc.ResponsePacket, sessionState, int) {
	var packets []rpc.RequestPacket
	if err := cbor.Unmarshal(raw, &packets); err != nil {
		var packet rpc.RequestPacket
		if singleErr := cbor.Unmarshal(raw, &packet); singleErr != nil {
			response := rpc.ResponsePacket{
				RequestID:  uuid.New(),
				EventType:  "rpcCallResponse",
				Parameters: map[string]any{"error": rpc.ErrorBody{Code: "bad_request", Message: "invalid cbor payload"}},
			}
			return []rpc.ResponsePacket{response}, state, http.StatusBadRequest
		}
		packets = []rpc.RequestPacket{packet}
	}

	responses, nextState, statusCode := h.handlePackets(ctx, packets, state)
	return responses, nextState, statusCode
}

func (h *Handler) handlePackets(ctx context.Context, packets []rpc.RequestPacket, state sessionState) ([]rpc.ResponsePacket, sessionState, int) {
	responses := make([]rpc.ResponsePacket, 0, len(packets)+4)
	statusCode := http.StatusOK
	currentState := state

	for _, packet := range packets {
		response, nextState, err := h.handlePacket(ctx, packet, currentState)
		currentState = nextState
		if err != nil {
			statusCode = maxStatus(statusCode, http.StatusBadRequest)
			responses = append(responses, response)
			continue
		}
		responses = append(responses, response)
	}

	if currentState.Authenticated {
		events, err := h.authService.PullEvents(ctx, appauth.PullEventsInput{UserPublicKey: currentState.UserPublicKey})
		if err != nil {
			h.logger.Error("failed to pull user events", "error", err)
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
	}

	return responses, currentState, statusCode
}

func (h *Handler) handlePacket(ctx context.Context, packet rpc.RequestPacket, state sessionState) (rpc.ResponsePacket, sessionState, error) {
	payload, err := rpc.DecodePayload(packet.Payload)
	if err != nil {
		return rpc.ResponsePacket{RequestID: uuid.New(), EventType: "rpcCallResponse", Parameters: errorParameters("bad_request", "invalid payload")}, state, err
	}

	nextState := state
	signer, err := h.resolveSigner(ctx, payload, state)
	if err != nil {
		return h.errorResponse(payload.RequestID, err), state, err
	}
	if len(signer) > 0 && !ed25519.Verify(signer, packet.Payload, packet.Signature) {
		return h.errorResponse(payload.RequestID, domainauth.ErrInvalidSignature), state, domainauth.ErrInvalidSignature
	}

	switch payload.RPCCall {
	case "requestAuthChallenge":
		var params requestAuthChallengeParams
		if err := rpc.DecodeParameters(payload.Parameters, &params); err != nil {
			return h.errorResponse(payload.RequestID, err), state, err
		}
		result, err := h.authService.RequestAuthChallenge(ctx, appauth.RequestAuthChallengeInput{
			UserPublicKey: params.UserPublicKey,
			PublicIP:      params.PublicIP,
			DeviceID:      params.DeviceID,
			ClientNonce:   params.ClientNonce,
		})
		if err != nil {
			return h.errorResponse(payload.RequestID, err), state, err
		}
		nextState.SessionID = result.SessionID
		nextState.UserPublicKey = append([]byte(nil), params.UserPublicKey...)
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
			return h.errorResponse(payload.RequestID, err), state, err
		}
		result, err := h.authService.SolveAuthChallenge(ctx, appauth.SolveAuthChallengeInput{
			SessionID: params.SessionID,
			Signature: params.Signature,
		})
		if err != nil {
			return h.errorResponse(payload.RequestID, err), state, err
		}
		nextState.SessionID = params.SessionID
		nextState.UserPublicKey = append([]byte(nil), result.UserPublicKey...)
		nextState.Authenticated = result.IsAuthenticated
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
			return h.errorResponse(payload.RequestID, err), state, err
		}
		_ = params
		result, err := h.authService.SubscribeToEvents(ctx, appauth.SubscribeToEventsInput{SessionID: nextState.SessionID})
		if err != nil {
			return h.errorResponse(payload.RequestID, err), state, err
		}
		nextState.SubscriptionIDs = append(nextState.SubscriptionIDs, result.SubscriptionID)
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
			return h.errorResponse(payload.RequestID, err), state, err
		}
		result, err := h.authService.UnsubscribeFromEvents(ctx, appauth.UnsubscribeFromEventsInput{SubscriptionID: params.SubscriptionID})
		if err != nil {
			return h.errorResponse(payload.RequestID, err), state, err
		}
		return rpc.ResponsePacket{
			RequestID:        uuid.New(),
			ReplyToRequestID: &payload.RequestID,
			EventType:        "rpcCallResponse",
			Parameters: map[string]any{
				"unsubscribedAt": result.UnsubscribedAt.UTC().Format(time.RFC3339Nano),
			},
		}, nextState, nil
	default:
		response, responseState, handledErr := h.handleClientAPICall(ctx, payload, nextState)
		if handledErr == nil {
			return response, responseState, nil
		}
		return h.errorResponse(payload.RequestID, handledErr), state, handledErr
	}
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
		errors.Is(err, domainauth.ErrInvalidClientNonce):
		return "invalid_argument", err.Error()
	case errors.Is(err, domainauth.ErrSessionNotFound),
		errors.Is(err, domainauth.ErrSubscriptionNotFound),
		errors.Is(err, clientapi.ErrNotFound):
		return "not_found", err.Error()
	case errors.Is(err, clientapi.ErrForbidden):
		return "forbidden", err.Error()
	case errors.Is(err, clientapi.ErrConflict):
		return "conflict", err.Error()
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
