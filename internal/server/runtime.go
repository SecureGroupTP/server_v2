package server

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"server_v2/internal/config"
	"server_v2/internal/platform/logging"
)

const (
	readTimeout           = 10 * time.Second
	writeTimeout          = 10 * time.Second
	idleTimeout           = 60 * time.Second
	tcpBannerWriteTimeout = 2 * time.Second
)

type Runtime struct {
	cfg           config.AppConfiguration
	handler       http.Handler
	streamHandler interface {
		HandleStream(ctx context.Context, rw io.ReadWriter) error
	}
	httpConnectionBinder *HTTPConnectionBinder
	logger               *slog.Logger

	errCh chan error

	httpServers  []*http.Server
	tcpListeners []net.Listener
	mu           sync.Mutex
}

func NewRuntime(
	cfg config.AppConfiguration,
	handler http.Handler,
	streamHandler interface {
		HandleStream(ctx context.Context, rw io.ReadWriter) error
	},
	httpConnectionBinder *HTTPConnectionBinder,
	logger *slog.Logger,
) *Runtime {
	return &Runtime{
		cfg:                  cfg,
		handler:              handler,
		streamHandler:        streamHandler,
		httpConnectionBinder: httpConnectionBinder,
		logger:               logging.WithSource(logger, "server_v2/internal/server.Runtime"),
		errCh:                make(chan error, 1),
	}
}

func (r *Runtime) Run(ctx context.Context) error {
	r.logger.Info("runtime starting")
	if err := r.startAll(); err != nil {
		r.logger.Error("runtime failed to start listeners", "error", err)
		return err
	}
	r.logger.Info("runtime started")

	select {
	case <-ctx.Done():
		r.logger.Info("runtime context canceled")
		return nil
	case err := <-r.errCh:
		r.logger.Error("runtime listener failed", "error", err)
		return err
	}
}

func (r *Runtime) Shutdown(ctx context.Context) error {
	startedAt := time.Now()
	r.mu.Lock()
	httpServers := append([]*http.Server(nil), r.httpServers...)
	tcpListeners := append([]net.Listener(nil), r.tcpListeners...)
	r.mu.Unlock()

	r.logger.Info("runtime shutdown started", "tcp_listeners", len(tcpListeners), "http_servers", len(httpServers))
	var shutdownErr error

	for _, listener := range tcpListeners {
		if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			r.logger.Warn("failed to close tcp listener", "addr", listener.Addr().String(), "error", err)
			shutdownErr = errors.Join(shutdownErr, err)
			continue
		}
		r.logger.Debug("tcp listener closed", "addr", listener.Addr().String())
	}

	for _, server := range httpServers {
		if err := server.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			r.logger.Warn("failed to shutdown http server", "addr", server.Addr, "error", err)
			shutdownErr = errors.Join(shutdownErr, err)
			continue
		}
		r.logger.Debug("http server shutdown completed", "addr", server.Addr)
	}

	if shutdownErr != nil {
		r.logger.Error("runtime shutdown completed with errors", "duration_ms", time.Since(startedAt).Milliseconds(), "error", shutdownErr)
		return shutdownErr
	}
	r.logger.Info("runtime shutdown completed", "duration_ms", time.Since(startedAt).Milliseconds())
	return shutdownErr
}

func (r *Runtime) startAll() error {
	if err := r.startTCPListener(r.cfg.Ports.TCPPort); err != nil {
		return err
	}

	for _, port := range uniquePorts(r.cfg.Ports.HTTPPort, r.cfg.Ports.WSPort) {
		if err := r.startHTTPServer(port); err != nil {
			return err
		}
	}

	return nil
}

func (r *Runtime) startHTTPServer(port int) error {
	addr := net.JoinHostPort(r.cfg.Host, strconv.Itoa(port))
	server := &http.Server{
		Addr:         addr,
		Handler:      r.handler,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
		IdleTimeout:  idleTimeout,
		ConnContext:  r.connContext,
		ConnState:    r.connState,
	}

	r.mu.Lock()
	r.httpServers = append(r.httpServers, server)
	r.mu.Unlock()

	r.logger.Info("starting HTTP/WS listener", "addr", addr, "read_timeout", readTimeout.String(), "write_timeout", writeTimeout.String(), "idle_timeout", idleTimeout.String())
	go func() {
		err := server.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			r.reportError(fmt.Errorf("http/ws server on %s: %w", addr, err))
		}
	}()

	return nil
}

func (r *Runtime) connContext(ctx context.Context, conn net.Conn) context.Context {
	if r.httpConnectionBinder == nil {
		return ctx
	}
	return r.httpConnectionBinder.ConnContext(ctx, conn)
}

func (r *Runtime) connState(conn net.Conn, state http.ConnState) {
	if r.httpConnectionBinder == nil {
		return
	}
	r.httpConnectionBinder.ConnState(conn, state)
}

func (r *Runtime) startTCPListener(port int) error {
	addr := net.JoinHostPort(r.cfg.Host, strconv.Itoa(port))

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}

	r.mu.Lock()
	r.tcpListeners = append(r.tcpListeners, listener)
	r.mu.Unlock()

	kind := "TCP"
	r.logger.Info("starting listener", "kind", kind, "addr", addr)

	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				if errors.Is(acceptErr, net.ErrClosed) {
					return
				}
				r.reportError(fmt.Errorf("accept %s on %s: %w", kind, addr, acceptErr))
				return
			}

			r.logger.Debug("tcp connection accepted", "kind", kind, "local_addr", conn.LocalAddr().String(), "remote_addr", conn.RemoteAddr().String())
			go r.handleTCPConnection(conn)
		}
	}()

	return nil
}

func (r *Runtime) reportError(err error) {
	r.logger.Error("runtime error reported", "error", err)
	select {
	case r.errCh <- err:
	default:
		r.logger.Warn("runtime error dropped because error channel is full", "error", err)
	}
}

func (r *Runtime) handleTCPConnection(conn net.Conn) {
	startedAt := time.Now()
	defer func() {
		_ = conn.Close()
		r.logger.Debug("tcp connection closed", "local_addr", conn.LocalAddr().String(), "remote_addr", conn.RemoteAddr().String(), "duration_ms", time.Since(startedAt).Milliseconds())
	}()
	if r.streamHandler == nil {
		r.logger.Warn("tcp connection closed without stream handler", "remote_addr", conn.RemoteAddr().String())
		return
	}
	banner := tcpDiscoveryBanner(r.cfg.OutputPorts)
	_ = conn.SetWriteDeadline(time.Now().Add(tcpBannerWriteTimeout))
	if err := writeAll(conn, banner[:]); err != nil {
		r.logger.Warn("tcp discovery banner write failed", "remote_addr", conn.RemoteAddr().String(), "error", err)
		return
	}
	_ = conn.SetWriteDeadline(time.Time{})
	if err := r.streamHandler.HandleStream(context.Background(), conn); err != nil && !errors.Is(err, net.ErrClosed) {
		r.logger.Warn("tcp client stream failed", "error", err)
	}
}

func tcpDiscoveryBanner(ports config.AppPortsConfiguration) [25]byte {
	var out [25]byte
	if ports.TCPPort > 0 {
		out[0] |= 1 << 0
	}
	if ports.TCPTLSPort > 0 {
		out[0] |= 1 << 1
	}
	if ports.HTTPPort > 0 {
		out[0] |= 1 << 2
	}
	if ports.HTTPSPort > 0 {
		out[0] |= 1 << 3
	}
	if ports.WSPort > 0 {
		out[0] |= 1 << 4
	}
	if ports.WSSPort > 0 {
		out[0] |= 1 << 5
	}
	binary.BigEndian.PutUint32(out[1:5], uint32(ports.TCPPort))
	binary.BigEndian.PutUint32(out[5:9], uint32(ports.TCPTLSPort))
	binary.BigEndian.PutUint32(out[9:13], uint32(ports.HTTPPort))
	binary.BigEndian.PutUint32(out[13:17], uint32(ports.HTTPSPort))
	binary.BigEndian.PutUint32(out[17:21], uint32(ports.WSPort))
	binary.BigEndian.PutUint32(out[21:25], uint32(ports.WSSPort))
	return out
}

func writeAll(w io.Writer, payload []byte) error {
	for len(payload) > 0 {
		n, err := w.Write(payload)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		payload = payload[n:]
	}
	return nil
}

func uniquePorts(first int, second int) []int {
	if first == second {
		return []int{first}
	}
	return []int{first, second}
}
