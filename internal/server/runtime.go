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
)

const (
	readTimeout  = 10 * time.Second
	writeTimeout = 10 * time.Second
	idleTimeout  = 60 * time.Second
)

type Runtime struct {
	cfg     config.AppConfiguration
	handler http.Handler
	logger  *slog.Logger

	errCh chan error

	httpServers  []*http.Server
	tcpListeners []net.Listener
	mu           sync.Mutex
}

func NewRuntime(cfg config.AppConfiguration, handler http.Handler, logger *slog.Logger) *Runtime {
	return &Runtime{
		cfg:     cfg,
		handler: handler,
		logger:  logger,
		errCh:   make(chan error, 1),
	}
}

func (r *Runtime) Run(ctx context.Context) error {
	if err := r.startAll(); err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return nil
	case err := <-r.errCh:
		return err
	}
}

func (r *Runtime) Shutdown(ctx context.Context) error {
	r.mu.Lock()
	httpServers := append([]*http.Server(nil), r.httpServers...)
	tcpListeners := append([]net.Listener(nil), r.tcpListeners...)
	r.mu.Unlock()

	var shutdownErr error

	for _, listener := range tcpListeners {
		if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			shutdownErr = errors.Join(shutdownErr, err)
		}
	}

	for _, server := range httpServers {
		if err := server.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			shutdownErr = errors.Join(shutdownErr, err)
		}
	}

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
	}

	r.mu.Lock()
	r.httpServers = append(r.httpServers, server)
	r.mu.Unlock()

	if withTLS {
		r.logger.Info("starting HTTPS/WSS listener", "addr", addr)
		go func() {
			err := server.ListenAndServeTLS(r.cfg.TLS.CertFile, r.cfg.TLS.KeyFile)
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				r.reportError(fmt.Errorf("https/wss server on %s: %w", addr, err))
			}
		}()
		return nil
	}

	r.logger.Info("starting HTTP/WS listener", "addr", addr)
	go func() {
		err := server.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			r.reportError(fmt.Errorf("http/ws server on %s: %w", addr, err))
		}
	}()

	return nil
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

			go handleTCPConnection(conn)
		}
	}()

	return nil
}

func (r *Runtime) buildTLSConfig() (*tls.Config, error) {
	certificate, err := tls.LoadX509KeyPair(r.cfg.TLS.CertFile, r.cfg.TLS.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("load tls cert/key: %w", err)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{certificate},
		MinVersion:   tls.VersionTLS12,
	}, nil
}

func (r *Runtime) reportError(err error) {
	select {
	case r.errCh <- err:
	default:
	}
}

func handleTCPConnection(conn net.Conn) {
	defer conn.Close()
	_, _ = io.Copy(conn, conn)
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
