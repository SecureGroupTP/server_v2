package auth

import (
	"encoding/binary"
	"fmt"
	"time"

	"github.com/fxamacker/cbor/v2"
)

const authenticationChallengeType = "authenticationChallenge"

type authChallengePayload struct {
	Type                string `cbor:"type"`
	ExpirationTimestamp uint64 `cbor:"expirationTimestamp"`
	ServerNonce         uint64 `cbor:"serverNonce"`
	ClientNonce         []byte `cbor:"clientNonce"`
}

func marshalAuthChallengePayload(expiresAt time.Time, serverNonce uint64, clientNonce []byte) ([]byte, error) {
	payload, err := cbor.Marshal(authChallengePayload{
		Type:                authenticationChallengeType,
		ExpirationTimestamp: uint64(expiresAt.UTC().UnixMicro()),
		ServerNonce:         serverNonce,
		ClientNonce:         append([]byte(nil), clientNonce...),
	})
	if err != nil {
		return nil, fmt.Errorf("marshal auth challenge payload: %w", err)
	}
	return payload, nil
}

func randomUint64(randomBytes []byte) uint64 {
	return binary.BigEndian.Uint64(randomBytes)
}
