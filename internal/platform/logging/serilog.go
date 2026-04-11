package logging

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
	"runtime"
	"strconv"
	"sync"
	"time"
)

const (
	fieldTimestamp     = "@t"
	fieldMessage       = "@m"
	fieldLevel         = "@l"
	fieldException     = "@x"
	fieldEventID       = "@i"
	fieldSourceContext = "SourceContext"
)

type SerilogHandler struct {
	out    io.Writer
	level  slog.Leveler
	attrs  []slog.Attr
	groups []string
	mu     *sync.Mutex
}

func NewSerilogHandler(out io.Writer, level slog.Leveler) *SerilogHandler {
	if level == nil {
		level = slog.LevelInfo
	}
	return &SerilogHandler{
		out:   out,
		level: level,
		mu:    &sync.Mutex{},
	}
}

func NewLogger(out io.Writer, level slog.Leveler) *slog.Logger {
	return slog.New(NewSerilogHandler(out, level))
}

func WithSource(logger *slog.Logger, sourceContext string) *slog.Logger {
	if logger == nil {
		logger = slog.Default()
	}
	return logger.With(fieldSourceContext, sourceContext)
}

func (h *SerilogHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level.Level()
}

func (h *SerilogHandler) Handle(_ context.Context, record slog.Record) error {
	event := map[string]any{
		fieldTimestamp:     record.Time.UTC().Format(time.RFC3339Nano),
		fieldMessage:       record.Message,
		fieldEventID:       eventID(record.Message),
		fieldSourceContext: sourceContext(record),
	}
	if record.Time.IsZero() {
		event[fieldTimestamp] = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if level := serilogLevel(record.Level); level != "" {
		event[fieldLevel] = level
	}

	for _, attr := range h.attrs {
		h.appendAttr(event, attr)
	}
	record.Attrs(func(attr slog.Attr) bool {
		h.appendAttr(event, attr)
		return true
	})

	if _, ok := event[fieldSourceContext]; !ok {
		event[fieldSourceContext] = sourceContext(record)
	}
	if _, ok := event[fieldEventID]; !ok {
		event[fieldEventID] = eventID(record.Message)
	}

	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal log event: %w", err)
	}
	payload = append(payload, '\n')

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err = h.out.Write(payload)
	return err
}

func (h *SerilogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := h.clone()
	next.attrs = append(next.attrs, attrs...)
	return next
}

func (h *SerilogHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	next := h.clone()
	next.groups = append(next.groups, name)
	return next
}

func (h *SerilogHandler) clone() *SerilogHandler {
	next := *h
	next.attrs = append([]slog.Attr(nil), h.attrs...)
	next.groups = append([]string(nil), h.groups...)
	return &next
}

func (h *SerilogHandler) appendAttr(event map[string]any, attr slog.Attr) {
	attr.Value = attr.Value.Resolve()
	if attr.Equal(slog.Attr{}) {
		return
	}
	key := attr.Key
	for i := len(h.groups) - 1; i >= 0; i-- {
		key = h.groups[i] + "." + key
	}
	if key == "" {
		return
	}

	if key == "error" && attr.Value.Kind() == slog.KindAny {
		if err, ok := attr.Value.Any().(error); ok && err != nil {
			event[fieldException] = err.Error()
			event[key] = err.Error()
			return
		}
	}
	event[key] = value(attr.Value)
}

func value(v slog.Value) any {
	switch v.Kind() {
	case slog.KindAny:
		switch typed := v.Any().(type) {
		case error:
			return typed.Error()
		default:
			return typed
		}
	case slog.KindBool:
		return v.Bool()
	case slog.KindDuration:
		return v.Duration().String()
	case slog.KindFloat64:
		return v.Float64()
	case slog.KindInt64:
		return v.Int64()
	case slog.KindString:
		return v.String()
	case slog.KindTime:
		return v.Time().UTC().Format(time.RFC3339Nano)
	case slog.KindUint64:
		return v.Uint64()
	case slog.KindGroup:
		group := make(map[string]any)
		for _, attr := range v.Group() {
			group[attr.Key] = value(attr.Value.Resolve())
		}
		return group
	case slog.KindLogValuer:
		return value(v.Resolve())
	default:
		return v.String()
	}
}

func serilogLevel(level slog.Level) string {
	switch {
	case level <= slog.LevelDebug:
		return "Debug"
	case level < slog.LevelWarn:
		return ""
	case level < slog.LevelError:
		return "Warning"
	case level < slog.LevelError+4:
		return "Error"
	default:
		return "Fatal"
	}
}

func eventID(message string) string {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(message))
	return fmt.Sprintf("%08x", hash.Sum32())
}

func sourceContext(record slog.Record) string {
	if record.PC == 0 {
		return "server_v2"
	}
	frame, _ := runtime.CallersFrames([]uintptr{record.PC}).Next()
	if frame.Function == "" {
		return "server_v2"
	}
	return frame.Function + ":" + strconv.Itoa(frame.Line)
}
