package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
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
