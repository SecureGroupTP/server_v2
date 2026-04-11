package server

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"server_v2/internal/config"
	"server_v2/internal/platform/logging"
)

const (
	readTimeout  = 10 * time.Second
	writeTimeout = 10 * time.Second
	idleTimeout  = 60 * time.Second
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
	if err := r.startTCPListener(r.cfg.Ports.TCPPort, false); err != nil {
		return err
	}

	for _, port := range uniquePorts(r.cfg.Ports.HTTPPort, r.cfg.Ports.WSPort) {
		if err := r.startHTTPServer(port, false); err != nil {
			return err
		}
	}

	tlsReady, reason := r.tlsReady()
	if tlsReady {
		if err := r.startTCPListener(r.cfg.Ports.TCPTLSPort, true); err != nil {
			return err
		}

		for _, port := range uniquePorts(r.cfg.Ports.HTTPSPort, r.cfg.Ports.WSSPort) {
			if err := r.startHTTPServer(port, true); err != nil {
				return err
			}
		}
	} else {
		r.logger.Warn(
			"TLS listeners are disabled",
			"reason", reason,
			"tcp_tls_port", r.cfg.Ports.TCPTLSPort,
			"https_port", r.cfg.Ports.HTTPSPort,
			"wss_port", r.cfg.Ports.WSSPort,
		)
	}

	return nil
}

func (r *Runtime) startHTTPServer(port int, withTLS bool) error {
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

	if withTLS {
		r.logger.Info("starting HTTPS/WSS listener", "addr", addr, "read_timeout", readTimeout.String(), "write_timeout", writeTimeout.String(), "idle_timeout", idleTimeout.String())
		go func() {
			err := server.ListenAndServeTLS(r.cfg.TLS.CertFile, r.cfg.TLS.KeyFile)
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				r.reportError(fmt.Errorf("https/wss server on %s: %w", addr, err))
			}
		}()
		return nil
	}

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

func (r *Runtime) startTCPListener(port int, withTLS bool) error {
	addr := net.JoinHostPort(r.cfg.Host, strconv.Itoa(port))

	var (
		listener net.Listener
		err      error
	)

	if withTLS {
		tlsConfig, cfgErr := r.buildTLSConfig()
		if cfgErr != nil {
			return cfgErr
		}
		listener, err = tls.Listen("tcp", addr, tlsConfig)
	} else {
		listener, err = net.Listen("tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}

	r.mu.Lock()
	r.tcpListeners = append(r.tcpListeners, listener)
	r.mu.Unlock()

	kind := "TCP"
	if withTLS {
		kind = "TCP/TLS"
	}
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

func (r *Runtime) buildTLSConfig() (*tls.Config, error) {
	certificate, err := tls.LoadX509KeyPair(r.cfg.TLS.CertFile, r.cfg.TLS.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("load tls cert/key: %w", err)
	}
	r.logger.Info("tls certificate loaded", "cert_file", r.cfg.TLS.CertFile, "key_file", r.cfg.TLS.KeyFile, "min_version", "TLS1.2")

	return &tls.Config{
		Certificates: []tls.Certificate{certificate},
		MinVersion:   tls.VersionTLS12,
	}, nil
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
	if err := r.streamHandler.HandleStream(context.Background(), conn); err != nil && !errors.Is(err, net.ErrClosed) {
		r.logger.Warn("tcp client stream failed", "error", err)
	}
}

func uniquePorts(first int, second int) []int {
	if first == second {
		return []int{first}
	}
	return []int{first, second}
}

func (r *Runtime) tlsReady() (bool, string) {
	certPath := strings.TrimSpace(r.cfg.TLS.CertFile)
	keyPath := strings.TrimSpace(r.cfg.TLS.KeyFile)
	if certPath == "" || keyPath == "" {
		return false, "cert_file or key_file is empty"
	}

	if _, err := os.Stat(certPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, "certificate file does not exist"
		}
		return false, fmt.Sprintf("cannot access certificate file: %v", err)
	}

	if _, err := os.Stat(keyPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, "key file does not exist"
		}
		return false, fmt.Sprintf("cannot access key file: %v", err)
	}

	return true, ""
}
