package clientrpc

import (
	"context"
	"fmt"

	"github.com/fxamacker/cbor/v2"
	"github.com/google/uuid"

	clientapi "server_v2/internal/application/clientapi"
	domainauth "server_v2/internal/domain/auth"
	"server_v2/internal/domain/rpc"
)

func (h *Handler) handleClientAPICall(ctx context.Context, payload rpc.RequestPayload, state sessionState) (rpc.ResponsePacket, sessionState, error) {
	if h.clientService == nil {
		return rpc.ResponsePacket{}, state, fmt.Errorf("rpc %s is not implemented yet", payload.RPCCall)
	}
	if !state.Authenticated {
		return rpc.ResponsePacket{}, state, domainauth.ErrSessionNotAuthenticated
	}
	if !state.ProfileCompleted && payload.RPCCall != "updateProfile" {
		return rpc.ResponsePacket{}, state, clientapi.ErrProfileRequired
	}

	params, err := decodeMapParameters(payload.Parameters)
	if err != nil {
		return rpc.ResponsePacket{}, state, err
	}

	responseParams, err := h.dispatchClientMethod(ctx, payload.RPCCall, params, state)
	if err != nil {
		return rpc.ResponsePacket{}, state, err
	}
	if payload.RPCCall == "updateProfile" {
		state.ProfileCompleted = true
	}
	return rpc.ResponsePacket{RequestID: uuid.New(), ReplyToRequestID: &payload.RequestID, EventType: "rpcCallResponse", Parameters: responseParams}, state, nil
}

func decodeMapParameters(raw []byte) (map[string]any, error) {
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	var params map[string]any
	if err := cbor.Unmarshal(raw, &params); err != nil {
		return nil, err
	}
	if params == nil {
		return map[string]any{}, nil
	}
	return params, nil
}

func mustUUID(params map[string]any, key string) uuid.UUID {
	value, ok := params[key]
	if !ok || value == nil {
		return uuid.Nil
	}
	switch typed := value.(type) {
	case uuid.UUID:
		return typed
	case string:
		parsed, _ := uuid.Parse(typed)
		return parsed
	case []byte:
		parsed, _ := uuid.FromBytes(typed)
		return parsed
	default:
		return uuid.Nil
	}
}

func optionalUUIDPtr(params map[string]any, key string) *uuid.UUID {
	value := mustUUID(params, key)
	if value == uuid.Nil {
		return nil
	}
	return &value
}

func mustBytes(params map[string]any, key string) []byte {
	value, ok := params[key]
	if !ok || value == nil {
		return nil
	}
	result, _ := value.([]byte)
	return result
}

func optionalBytes(params map[string]any, key string) []byte {
	return mustBytes(params, key)
}

func mustBytesList(params map[string]any, key string) [][]byte {
	value, ok := params[key]
	if !ok || value == nil {
		return nil
	}
	rawList, ok := value.([]any)
	if !ok {
		if typed, ok := value.([][]byte); ok {
			return typed
		}
		return nil
	}
	out := make([][]byte, 0, len(rawList))
	for _, item := range rawList {
		bytesValue, _ := item.([]byte)
		out = append(out, bytesValue)
	}
	return out
}

func mustMapList(params map[string]any, key string) []map[string]any {
	value, ok := params[key]
	if !ok || value == nil {
		return nil
	}
	rawList, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(rawList))
	for _, item := range rawList {
		mapped, _ := item.(map[string]any)
		out = append(out, mapped)
	}
	return out
}

func optionalString(params map[string]any, key string) string {
	value, ok := params[key]
	if !ok || value == nil {
		return ""
	}
	result, _ := value.(string)
	return result
}

func optionalInt(params map[string]any, key string) int {
	value, ok := params[key]
	if !ok || value == nil {
		return 0
	}
	switch typed := value.(type) {
	case uint64:
		return int(typed)
	case uint32:
		return int(typed)
	case int64:
		return int(typed)
	case int:
		return typed
	case int32:
		return int(typed)
	case uint16:
		return int(typed)
	default:
		return 0
	}
}

func optionalBool(params map[string]any, key string) bool {
	value, ok := params[key]
	if !ok || value == nil {
		return false
	}
	result, _ := value.(bool)
	return result
}
