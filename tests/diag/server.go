package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"sync"
	"time"

	_ "expvar"

	"github.com/voluminor/ratatoskr"
)

// // // // // // // // // //

const (
	// envDiagToken, when non-empty at startup, requires a matching X-Diag-Token
	// header on every state-changing endpoint.
	envDiagToken = "RTS_DIAG_TOKEN"
	// envDiagDebug enables the pprof/expvar debug listener regardless of config.
	envDiagDebug = "RTS_DIAG_DEBUG"
	// diagTokenHeader carries the shared secret for mutating endpoints.
	diagTokenHeader = "X-Diag-Token"
)

// serverObj owns diagnostic HTTP surfaces and echo listeners.
type serverObj struct {
	cfg         ConfigObj
	node        *ratatoskr.Obj
	log         loggerObj
	startedAt   time.Time
	httpServer  *http.Server
	debugServer *http.Server
	cancel      context.CancelFunc
	shutdown    context.CancelFunc
	wg          sync.WaitGroup
	mutateToken string
	debugOn     bool
	throughput  *throughputRegistryObj
	gomaxprocs  gomaxprocsControllerObj
}

func newServer(cfg ConfigObj, node *ratatoskr.Obj, log loggerObj, shutdown context.CancelFunc) *serverObj {
	return &serverObj{
		cfg:         cfg,
		node:        node,
		log:         log,
		startedAt:   time.Now(),
		shutdown:    shutdown,
		mutateToken: os.Getenv(envDiagToken),
		debugOn:     cfg.DebugEnabled || os.Getenv(envDiagDebug) != "",
		throughput:  &throughputRegistryObj{},
	}
}

// requirePOST rejects non-POST requests on state-changing endpoints.
func requirePOST(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, statusObj{OK: false, Error: "method not allowed"})
		return false
	}
	return true
}

// guardMutation enforces POST and, when a token is configured, a matching
// X-Diag-Token header using a constant-time comparison.
func (s *serverObj) guardMutation(w http.ResponseWriter, r *http.Request) bool {
	if !requirePOST(w, r) {
		return false
	}
	if s.mutateToken == "" {
		return true
	}
	got := r.Header.Get(diagTokenHeader)
	if subtle.ConstantTimeCompare([]byte(got), []byte(s.mutateToken)) != 1 {
		writeJSON(w, http.StatusUnauthorized, statusObj{OK: false, Error: "invalid diagnostic token"})
		return false
	}
	return true
}

func (s *serverObj) start(parent context.Context) error {
	ctx, cancel := context.WithCancel(parent)
	s.cancel = cancel
	if err := s.startTCPEcho(ctx); err != nil {
		return err
	}
	if err := s.startUDPEcho(ctx); err != nil {
		return err
	}
	if err := s.startThroughputSinks(ctx); err != nil {
		return err
	}
	s.startHTTP(ctx)
	if s.debugOn {
		s.startDebug(ctx)
	} else {
		s.log.Infof("debug HTTP (pprof/expvar) disabled; set %s or debug_enabled to enable", envDiagDebug)
	}
	return nil
}

func (s *serverObj) close() {
	if s.cancel != nil {
		s.cancel()
	}
	_ = s.node.Close()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if s.httpServer != nil {
		_ = s.httpServer.Shutdown(shutdownCtx)
	}
	if s.debugServer != nil {
		_ = s.debugServer.Shutdown(shutdownCtx)
	}
	s.wg.Wait()
	s.gomaxprocs.restore()
}

func (s *serverObj) startHTTP(ctx context.Context) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/snapshot", s.handleSnapshot)
	mux.HandleFunc("/runtime", s.handleRuntime)
	mux.HandleFunc("/runtime/gomaxprocs", s.handleGOMAXPROCS)
	mux.HandleFunc("/check/tcp", s.handleTCPCheck)
	mux.HandleFunc("/check/udp", s.handleUDPCheck)
	mux.HandleFunc("/load/tcp", s.handleTCPLoad)
	mux.HandleFunc("/load/udp", s.handleUDPLoad)
	mux.HandleFunc("/throughput/start", s.handleThroughputStart)
	mux.HandleFunc("/throughput/run", s.handleThroughputRun)
	mux.HandleFunc("/throughput/finish", s.handleThroughputFinish)
	mux.HandleFunc("/socks/enable", s.handleSOCKSEnable)
	mux.HandleFunc("/socks/disable", s.handleSOCKSDisable)
	mux.HandleFunc("/close", s.handleClose)
	s.httpServer = &http.Server{
		Addr:              s.cfg.HTTPListen,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.log.Infof("diagnostic HTTP listening on %s", s.cfg.HTTPListen)
		if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.log.Errorf("diagnostic HTTP error: %v", err)
		}
	}()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.httpServer.Shutdown(shutdownCtx)
	}()
}

func (s *serverObj) startDebug(ctx context.Context) {
	s.debugServer = &http.Server{
		Addr:              s.cfg.DebugListen,
		Handler:           http.DefaultServeMux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.log.Infof("debug HTTP listening on %s", s.cfg.DebugListen)
		if err := s.debugServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.log.Errorf("debug HTTP error: %v", err)
		}
	}()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.debugServer.Shutdown(shutdownCtx)
	}()
}

func (s *serverObj) yggAddress(port uint16) string {
	return fmt.Sprintf("[%s]:%d", s.node.Address(), port)
}
