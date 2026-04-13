package clientrpc

import "testing"

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
