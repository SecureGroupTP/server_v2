package logging

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"
)

func TestSerilogHandlerWritesCompactJSON(t *testing.T) {
	var out bytes.Buffer
	logger := WithSource(NewLogger(&out, slog.LevelDebug), "test/source")

	logger.Info("service started", "port", 8080)

	var event map[string]any
	if err := json.Unmarshal(out.Bytes(), &event); err != nil {
		t.Fatalf("unmarshal log event: %v", err)
	}

	if event["@t"] == "" {
		t.Fatalf("expected @t to be set")
	}
	if event["@m"] != "service started" {
		t.Fatalf("unexpected @m: %v", event["@m"])
	}
	if event["@i"] == "" {
		t.Fatalf("expected @i to be set")
	}
	if event["SourceContext"] != "test/source" {
		t.Fatalf("unexpected SourceContext: %v", event["SourceContext"])
	}
	if _, ok := event["@l"]; ok {
		t.Fatalf("did not expect @l for information logs")
	}
	if event["port"] != float64(8080) {
		t.Fatalf("unexpected port: %v", event["port"])
	}
}

func TestSerilogHandlerMapsWarningAndError(t *testing.T) {
	var out bytes.Buffer
	logger := WithSource(NewLogger(&out, slog.LevelDebug), "test/source")

	logger.Warn("retry scheduled")

	var event map[string]any
	if err := json.Unmarshal(out.Bytes(), &event); err != nil {
		t.Fatalf("unmarshal log event: %v", err)
	}
	if event["@l"] != "Warning" {
		t.Fatalf("unexpected @l: %v", event["@l"])
	}
}

func TestSerilogHandlerGroupsAttrsErrorsAndValueKinds(t *testing.T) {
	var out bytes.Buffer
	handler := NewSerilogHandler(&out, slog.LevelDebug).
		WithGroup("request").
		WithAttrs([]slog.Attr{
			slog.String("id", "req-1"),
			slog.Bool("ok", true),
			slog.Duration("elapsed", 1500*time.Millisecond),
			slog.Float64("ratio", 1.5),
			slog.Int64("count", 7),
			slog.Uint64("size", 9),
			slog.Time("at", time.Date(2026, 4, 12, 13, 0, 0, 0, time.UTC)),
			slog.Group("nested", slog.String("key", "value")),
		})
	logger := slog.New(handler)

	logger.Error("failed request", "error", errors.New("boom"), "plainErr", errors.New("plain"))

	var event map[string]any
	if err := json.Unmarshal(out.Bytes(), &event); err != nil {
		t.Fatalf("unmarshal log event: %v", err)
	}
	if event["@l"] != "Error" || event["request.error"] != "boom" {
		t.Fatalf("unexpected error fields: %#v", event)
	}
	if event["request.id"] != "req-1" || event["request.ok"] != true || event["request.elapsed"] != "1.5s" || event["request.count"] != float64(7) || event["request.size"] != float64(9) {
		t.Fatalf("unexpected grouped fields: %#v", event)
	}
	nested, ok := event["request.nested"].(map[string]any)
	if !ok || nested["key"] != "value" {
		t.Fatalf("unexpected nested group: %#v", event["request.nested"])
	}
	if event["request.plainErr"] != "plain" {
		t.Fatalf("unexpected plain error value: %#v", event)
	}
}

func TestSerilogHelpersAndNilSource(t *testing.T) {
	var out bytes.Buffer
	logger := WithSource(nil, "fallback/source")
	if logger == nil {
		t.Fatal("expected fallback logger")
	}

	handler := NewSerilogHandler(&out, nil)
	if handler.Enabled(nil, slog.LevelDebug) {
		t.Fatal("nil level should default to info")
	}
	emptyGroup := handler.WithGroup("")
	if emptyGroup != handler {
		t.Fatal("empty group should return the original handler")
	}
	record := slog.NewRecord(time.Time{}, slog.LevelError+4, "fatal-ish", 0)
	if err := handler.Handle(nil, record); err != nil {
		t.Fatalf("handle fatal-ish record: %v", err)
	}
	var event map[string]any
	if err := json.Unmarshal(out.Bytes(), &event); err != nil {
		t.Fatalf("unmarshal fallback event: %v", err)
	}
	if event["@l"] != "Fatal" || event["SourceContext"] != "server_v2" {
		t.Fatalf("unexpected fallback event: %#v", event)
	}
}
