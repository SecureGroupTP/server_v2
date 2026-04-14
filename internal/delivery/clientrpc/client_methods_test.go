package clientrpc

import (
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/google/uuid"
)

func TestMustMapListNormalizesNestedMaps(t *testing.T) {
	params := map[string]any{
		"packages": []any{
			map[any]any{
				"expiresAt":    int64(123),
				"isLastResort": true,
				"meta": map[any]any{
					"label": "demo",
				},
			},
		},
	}

	got := mustMapList(params, "packages")
	if len(got) != 1 {
		t.Fatalf("expected one item, got %d", len(got))
	}
	if got[0] == nil {
		t.Fatal("expected normalized map item")
	}
	if _, ok := got[0]["expiresAt"].(int64); !ok {
		t.Fatalf("expected expiresAt int64, got %#v", got[0]["expiresAt"])
	}
	meta, ok := got[0]["meta"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested map[string]any, got %#v", got[0]["meta"])
	}
	if meta["label"] != "demo" {
		t.Fatalf("unexpected nested value: %#v", meta["label"])
	}
}

func TestClientMethodParameterHelpers(t *testing.T) {
	id := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	idBytes, _ := id.MarshalBinary()
	now := time.Date(2026, 4, 12, 14, 0, 0, 123000000, time.UTC)

	if got := mustUUID(map[string]any{"id": id}, "id"); got != id {
		t.Fatalf("uuid value: got %s", got)
	}
	if got := mustUUID(map[string]any{"id": id.String()}, "id"); got != id {
		t.Fatalf("uuid string: got %s", got)
	}
	if got := mustUUID(map[string]any{"id": idBytes}, "id"); got != id {
		t.Fatalf("uuid bytes: got %s", got)
	}
	if got := optionalUUIDPtr(map[string]any{"id": id.String()}, "id"); got == nil || *got != id {
		t.Fatalf("optional uuid ptr: %#v", got)
	}
	if got := optionalUUIDPtr(map[string]any{}, "id"); got != nil {
		t.Fatalf("expected nil uuid ptr, got %#v", got)
	}

	if got := mustBytesList(map[string]any{"items": [][]byte{[]byte("a")}}, "items"); len(got) != 1 || string(got[0]) != "a" {
		t.Fatalf("bytes list typed: %#v", got)
	}
	if got := mustBytesList(map[string]any{"items": []any{[]byte("b"), "bad"}}, "items"); len(got) != 2 || string(got[0]) != "b" || got[1] != nil {
		t.Fatalf("bytes list any: %#v", got)
	}
	if got := mustMapList(map[string]any{"items": []map[string]any{{"a": 1}}}, "items"); len(got) != 1 || got[0]["a"] != 1 {
		t.Fatalf("map list typed: %#v", got)
	}
	if got := mustMapList(map[string]any{"items": "bad"}, "items"); got != nil {
		t.Fatalf("expected nil map list, got %#v", got)
	}

	normalized := normalizeValue([]any{map[any]any{"x": []any{map[string]any{"y": 1}}}}).([]any)
	nested := normalized[0].(map[string]any)["x"].([]any)[0].(map[string]any)
	if nested["y"] != 1 {
		t.Fatalf("unexpected normalized value: %#v", normalized)
	}
	if mapped, ok := normalizeMap("bad"); ok || mapped != nil {
		t.Fatalf("expected unhandled map false, got %#v %v", mapped, ok)
	}

	for _, item := range []struct {
		value any
		want  int
	}{
		{uint64(1), 1},
		{uint32(2), 2},
		{int64(3), 3},
		{int(4), 4},
		{int32(5), 5},
		{uint16(6), 6},
		{"bad", 0},
		{nil, 0},
	} {
		if got := optionalInt(map[string]any{"v": item.value}, "v"); got != item.want {
			t.Fatalf("optional int %#v: got %d want %d", item.value, got, item.want)
		}
	}
	if !optionalBool(map[string]any{"v": true}, "v") || optionalBool(map[string]any{"v": "true"}, "v") {
		t.Fatal("unexpected optional bool result")
	}

	for _, value := range []any{
		now,
		now.Format(time.RFC3339Nano),
		now.UnixMicro(),
		int(now.UnixMicro()),
		int32(123),
		uint64(now.UnixMicro()),
		uint32(123),
		uint(123),
	} {
		if got := optionalTimePtr(map[string]any{"t": value}, "t"); got == nil {
			t.Fatalf("expected parsed time for %#v", value)
		}
	}
	for _, value := range []any{"not-time", []byte("bad"), nil} {
		if got := optionalTimePtr(map[string]any{"t": value}, "t"); got != nil {
			t.Fatalf("expected nil time for %#v, got %s", value, got)
		}
	}
}

func TestDecodeMapParametersEmptyNilAndInvalid(t *testing.T) {
	if got, err := decodeMapParameters(nil); err != nil || len(got) != 0 {
		t.Fatalf("empty params: got=%#v err=%v", got, err)
	}
	payload, err := cbor.Marshal(map[string]any(nil))
	if err != nil {
		t.Fatalf("marshal nil map: %v", err)
	}
	if got, err := decodeMapParameters(payload); err != nil || len(got) != 0 {
		t.Fatalf("nil map params: got=%#v err=%v", got, err)
	}
	if _, err := decodeMapParameters([]byte{0xff}); err == nil {
		t.Fatal("expected invalid cbor error")
	}
}
