package clientapi

import (
	"testing"
	"time"
)

func TestParseTimeValueAcceptsRFC3339String(t *testing.T) {
	want := time.Date(2026, 5, 13, 15, 8, 29, 692523000, time.UTC)
	got, err := parseTimeValue("2026-05-13T15:08:29.692523Z")
	if err != nil {
		t.Fatalf("parseTimeValue string: %v", err)
	}
	if !got.Equal(want) {
		t.Fatalf("unexpected time: got %s want %s", got, want)
	}
}

func TestParseTimeValueAcceptsUnixMicroseconds(t *testing.T) {
	want := time.Date(2026, 5, 13, 15, 8, 29, 692523000, time.UTC)
	got, err := parseTimeValue(want.UnixMicro())
	if err != nil {
		t.Fatalf("parseTimeValue int64: %v", err)
	}
	if !got.Equal(want) {
		t.Fatalf("unexpected time: got %s want %s", got, want)
	}
}
