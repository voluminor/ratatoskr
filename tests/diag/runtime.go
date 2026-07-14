package main

import (
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"sync"
	"time"
)

// // // // // // // // // //

// runtimeObj captures runtime counters that are useful before opening pprof.
type runtimeObj struct {
	Goroutines int               `json:"goroutines"`
	NumCPU     int               `json:"num_cpu"`
	GOMAXPROCS int               `json:"gomaxprocs"`
	Overridden bool              `json:"gomaxprocs_overridden"`
	GoVersion  string            `json:"go_version"`
	Mem        runtime.MemStats  `json:"mem"`
	Uptime     string            `json:"uptime"`
	Config     runtimeConfigObj  `json:"config"`
	Extra      map[string]uint64 `json:"extra,omitempty"`
}

// runtimeConfigObj exposes the diagnostic ports without echoing full config.
type runtimeConfigObj struct {
	HTTPListen  string `json:"http_listen"`
	DebugListen string `json:"debug_listen"`
	SOCKSListen string `json:"socks_listen"`
}

type gomaxprocsRequestObj struct {
	Value   *int `json:"value,omitempty"`
	Restore bool `json:"restore,omitempty"`
}

type gomaxprocsResponseObj struct {
	OK         bool   `json:"ok"`
	Previous   int    `json:"previous,omitempty"`
	Current    int    `json:"current,omitempty"`
	NumCPU     int    `json:"num_cpu"`
	Overridden bool   `json:"overridden"`
	Error      string `json:"error,omitempty"`
}

type gomaxprocsControllerObj struct {
	mu             sync.Mutex
	initialized    bool
	original       int
	restoreDefault bool
	overridden     bool
}

func gomaxprocsUsesRuntimeDefault() bool {
	value, ok := os.LookupEnv("GOMAXPROCS")
	if !ok {
		return true
	}
	parsed, err := strconv.Atoi(value)
	return err != nil || parsed < 1
}

func (c *gomaxprocsControllerObj) initializeLocked() {
	if c.initialized {
		return
	}
	c.initialized = true
	c.original = runtime.GOMAXPROCS(0)
	c.restoreDefault = gomaxprocsUsesRuntimeDefault()
}

func (c *gomaxprocsControllerObj) snapshot() (current, numCPU int, overridden bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.initializeLocked()
	return runtime.GOMAXPROCS(0), runtime.NumCPU(), c.overridden
}

func (c *gomaxprocsControllerObj) set(value int) gomaxprocsResponseObj {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.initializeLocked()
	numCPU := runtime.NumCPU()
	if value < 1 || value > numCPU {
		return gomaxprocsResponseObj{NumCPU: numCPU, Overridden: c.overridden, Error: fmt.Sprintf("GOMAXPROCS must be between 1 and %d", numCPU)}
	}
	previous := runtime.GOMAXPROCS(value)
	c.overridden = true
	return gomaxprocsResponseObj{OK: true, Previous: previous, Current: runtime.GOMAXPROCS(0), NumCPU: numCPU, Overridden: true}
}

func (c *gomaxprocsControllerObj) restore() gomaxprocsResponseObj {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.initializeLocked()
	previous := runtime.GOMAXPROCS(0)
	if c.restoreDefault {
		runtime.SetDefaultGOMAXPROCS()
	} else {
		runtime.GOMAXPROCS(c.original)
	}
	c.overridden = false
	return gomaxprocsResponseObj{OK: true, Previous: previous, Current: runtime.GOMAXPROCS(0), NumCPU: runtime.NumCPU()}
}

// //

func (s *serverObj) handleRuntime(w http.ResponseWriter, _ *http.Request) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	gomaxprocs, numCPU, overridden := s.gomaxprocs.snapshot()
	writeJSON(w, http.StatusOK, runtimeObj{
		Goroutines: runtime.NumGoroutine(),
		NumCPU:     numCPU,
		GOMAXPROCS: gomaxprocs,
		Overridden: overridden,
		GoVersion:  runtime.Version(),
		Mem:        mem,
		Uptime:     time.Since(s.startedAt).String(),
		Config: runtimeConfigObj{
			HTTPListen:  s.cfg.HTTPListen,
			DebugListen: s.cfg.DebugListen,
			SOCKSListen: s.cfg.SOCKSListen,
		},
	})
}

func (s *serverObj) handleGOMAXPROCS(w http.ResponseWriter, r *http.Request) {
	if !s.guardMutation(w, r) {
		return
	}
	var req gomaxprocsRequestObj
	if err := decodeJSON(w, r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, gomaxprocsResponseObj{NumCPU: runtime.NumCPU(), Error: err.Error()})
		return
	}
	if (req.Value == nil && !req.Restore) || (req.Value != nil && req.Restore) {
		writeJSON(w, http.StatusBadRequest, gomaxprocsResponseObj{NumCPU: runtime.NumCPU(), Error: "specify exactly one of value or restore"})
		return
	}
	result := s.gomaxprocs.restore()
	if req.Value != nil {
		result = s.gomaxprocs.set(*req.Value)
	}
	code := http.StatusOK
	if !result.OK {
		code = http.StatusBadRequest
	}
	writeJSON(w, code, result)
}
