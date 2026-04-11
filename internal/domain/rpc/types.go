package rpc

import (
	"encoding/json"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/google/uuid"
)

type RequestPacket struct {
	Signature []byte          `cbor:"signature"`
	Payload   cbor.RawMessage `cbor:"payload"`
}

type RequestPayload struct {
	RequestID  uuid.UUID       `cbor:"requestId"`
	RPCCall    string          `cbor:"rpcCall"`
	Timestamp  int64           `cbor:"timestamp"`
	Version    uint64          `cbor:"version"`
	Parameters cbor.RawMessage `cbor:"parameters"`
}

type ResponsePacket struct {
	RequestID        uuid.UUID      `cbor:"requestId" json:"requestId"`
	ReplyToRequestID *uuid.UUID     `cbor:"replyToRequestId" json:"replyToRequestId"`
	EventType        string         `cbor:"eventType" json:"eventType"`
	Parameters       map[string]any `cbor:"parameters" json:"parameters"`
}

type ErrorBody struct {
	Code    string `cbor:"code" json:"code"`
	Message string `cbor:"message" json:"message"`
	Retry   bool   `cbor:"retry" json:"retry"`
}

func DecodePayload(raw []byte) (RequestPayload, error) {
	var payload RequestPayload
	if err := cbor.Unmarshal(raw, &payload); err != nil {
		return RequestPayload{}, err
	}
	return payload, nil
}

func DecodeParameters(raw []byte, out any) error {
	return cbor.Unmarshal(raw, out)
}

func ParametersFromJSON(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}

	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]any{"raw": string(raw)}
	}
	if out == nil {
		return map[string]any{}
	}
	return out
}

func TimeToMillis(value time.Time) int64 {
	return value.UTC().UnixMilli()
}
