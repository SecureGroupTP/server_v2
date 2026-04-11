package server

import (
	"bufio"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"server_v2/internal/config"
	"server_v2/internal/platform/logging"
)

type clientRouteRegistrar interface {
	Register(mux *http.ServeMux)
}

type routeGroup struct {
	logger      *slog.Logger
	outputPorts config.AppPortsConfiguration
}

func NewHandler(
	logger *slog.Logger,
	outputPorts config.AppPortsConfiguration,
	clientHandler clientRouteRegistrar,
) http.Handler {
	group := routeGroup{
		logger:      logging.WithSource(logger, "server_v2/internal/server.routeGroup"),
		outputPorts: outputPorts,
	}

	mux := http.NewServeMux()
	group.registerDiscoveryRoutes(mux)
	if clientHandler != nil {
		clientHandler.Register(mux)
	}

	return group.accessLogMiddleware(mux)
}

type statusCapturingResponseWriter struct {
	http.ResponseWriter
	statusCode int
	bytes      int
}

func (w *statusCapturingResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *statusCapturingResponseWriter) Write(payload []byte) (int, error) {
	if w.statusCode == 0 {
		w.statusCode = http.StatusOK
	}
	written, err := w.ResponseWriter.Write(payload)
	w.bytes += written
	return written, err
}

func (w *statusCapturingResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *statusCapturingResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("response writer does not support hijacking")
	}
	return hijacker.Hijack()
}

func (g routeGroup) accessLogMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()
		recorder := &statusCapturingResponseWriter{ResponseWriter: w}
		next.ServeHTTP(recorder, r)
		statusCode := recorder.statusCode
		if statusCode == 0 {
			statusCode = http.StatusOK
		}

		g.logger.Info(
			"http request completed",
			"method", r.Method,
			"path", r.URL.Path,
			"status_code", statusCode,
			"response_bytes", recorder.bytes,
			"duration_ms", time.Since(startedAt).Milliseconds(),
			"remote_addr", r.RemoteAddr,
			"user_agent", r.UserAgent(),
		)
	})
}
