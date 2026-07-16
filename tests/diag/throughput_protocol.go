package main

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// // // // // // // // // //

const (
	throughputMagic          = "RTSTHR01"
	throughputRunIDBytes     = 16
	throughputTCPHeaderSize  = len(throughputMagic) + throughputRunIDBytes + 4
	throughputUDPHeaderSize  = throughputTCPHeaderSize + 8
	throughputSequenceWindow = 4096
	maxThroughputRuns        = 32
	throughputRunTTL         = 2 * time.Minute
)

var (
	errInvalidThroughputRunID = errors.New("invalid throughput run ID")
	errThroughputRunExists    = errors.New("throughput run already exists")
	errThroughputRunNotFound  = errors.New("throughput run not found")
	errTooManyThroughputRuns  = errors.New("too many throughput runs")
)

// //

type throughputHeaderObj struct {
	RunID    string
	StreamID uint32
	Sequence uint64
}

type throughputRunObj struct {
	id            string
	transport     string
	network       string
	expiresAt     time.Time
	receivedBytes atomic.Uint64
	tcpConns      atomic.Uint64
	active        atomic.Bool
	mu            sync.Mutex
	udp           map[uint32]*udpSequenceObj
}

type udpSequenceObj struct {
	bits        [throughputSequenceWindow / 64]uint64
	max         uint64
	initialized bool
	unique      uint64
	duplicates  uint64
	reordered   uint64
	tooOld      uint64
}

type throughputRegistryObj struct {
	mu    sync.Mutex
	runs  sync.Map
	count int
}

type throughputReceiverObj struct {
	OK              bool   `json:"ok"`
	ID              string `json:"id"`
	Transport       string `json:"transport"`
	Network         string `json:"network"`
	ReceivedBytes   uint64 `json:"received_bytes"`
	ReceivedPackets uint64 `json:"received_packets"`
	Connections     uint64 `json:"connections,omitempty"`
	Duplicates      uint64 `json:"duplicates"`
	Reordered       uint64 `json:"reordered"`
	TooOld          uint64 `json:"too_old"`
	Error           string `json:"error,omitempty"`
}

func validThroughputPath(transport, network string) bool {
	return (transport == "direct" || transport == "ygg") && (network == "tcp" || network == "udp")
}

func decodeThroughputRunID(id string) ([throughputRunIDBytes]byte, error) {
	var out [throughputRunIDBytes]byte
	decoded, err := hex.DecodeString(id)
	if err != nil || len(decoded) != throughputRunIDBytes {
		return out, errInvalidThroughputRunID
	}
	copy(out[:], decoded)
	return out, nil
}

func encodeThroughputHeader(dst []byte, id string, streamID uint32, sequence uint64, udp bool) error {
	want := throughputTCPHeaderSize
	if udp {
		want = throughputUDPHeaderSize
	}
	if len(dst) < want {
		return fmt.Errorf("throughput header buffer: got %d bytes, need %d", len(dst), want)
	}
	runID, err := decodeThroughputRunID(id)
	if err != nil {
		return err
	}
	copy(dst, throughputMagic)
	copy(dst[len(throughputMagic):], runID[:])
	offset := len(throughputMagic) + throughputRunIDBytes
	binary.BigEndian.PutUint32(dst[offset:], streamID)
	if udp {
		binary.BigEndian.PutUint64(dst[throughputTCPHeaderSize:], sequence)
	}
	return nil
}

func decodeThroughputHeader(src []byte, udp bool) (throughputHeaderObj, error) {
	want := throughputTCPHeaderSize
	if udp {
		want = throughputUDPHeaderSize
	}
	if len(src) < want || string(src[:len(throughputMagic)]) != throughputMagic {
		return throughputHeaderObj{}, errors.New("invalid throughput header")
	}
	runID := hex.EncodeToString(src[len(throughputMagic) : len(throughputMagic)+throughputRunIDBytes])
	offset := len(throughputMagic) + throughputRunIDBytes
	header := throughputHeaderObj{RunID: runID, StreamID: binary.BigEndian.Uint32(src[offset:])}
	if udp {
		header.Sequence = binary.BigEndian.Uint64(src[throughputTCPHeaderSize:])
	}
	return header, nil
}

func newThroughputRun(id, transport, network string, now time.Time) (*throughputRunObj, error) {
	if _, err := decodeThroughputRunID(id); err != nil {
		return nil, err
	}
	if !validThroughputPath(transport, network) {
		return nil, errors.New("invalid throughput transport or network")
	}
	return &throughputRunObj{
		id:        id,
		transport: transport,
		network:   network,
		expiresAt: now.Add(throughputRunTTL),
		udp:       make(map[uint32]*udpSequenceObj),
	}, nil
}

func (r *throughputRunObj) recordUDP(streamID uint32, sequence uint64, payloadBytes int) {
	r.mu.Lock()
	if !r.active.Load() {
		r.mu.Unlock()
		return
	}
	tracker := r.udp[streamID]
	if tracker == nil {
		tracker = &udpSequenceObj{}
		r.udp[streamID] = tracker
	}
	if tracker.record(sequence) {
		r.receivedBytes.Add(uint64(payloadBytes))
	}
	r.mu.Unlock()
}

func (r *throughputRunObj) snapshot() throughputReceiverObj {
	result := throughputReceiverObj{
		OK:            r.receivedBytes.Load() > 0,
		ID:            r.id,
		Transport:     r.transport,
		Network:       r.network,
		ReceivedBytes: r.receivedBytes.Load(),
		Connections:   r.tcpConns.Load(),
	}
	if r.network != "udp" {
		return result
	}
	r.mu.Lock()
	for _, tracker := range r.udp {
		result.ReceivedPackets += tracker.unique
		result.Duplicates += tracker.duplicates
		result.Reordered += tracker.reordered
		result.TooOld += tracker.tooOld
	}
	r.mu.Unlock()
	return result
}

func (s *udpSequenceObj) record(sequence uint64) bool {
	if !s.initialized {
		s.initialized = true
		s.max = sequence
		s.set(sequence)
		s.unique++
		return true
	}
	if sequence > s.max {
		delta := sequence - s.max
		if delta >= throughputSequenceWindow {
			clear(s.bits[:])
		} else {
			for current := s.max + 1; current <= sequence; current++ {
				s.clear(current)
			}
		}
		s.max = sequence
		s.set(sequence)
		s.unique++
		return true
	}
	if s.max-sequence >= throughputSequenceWindow {
		s.tooOld++
		return false
	}
	if s.has(sequence) {
		s.duplicates++
		return false
	}
	s.set(sequence)
	s.unique++
	s.reordered++
	return true
}

func (s *udpSequenceObj) has(sequence uint64) bool {
	word, bit := sequence%throughputSequenceWindow/64, sequence%64
	return s.bits[word]&(uint64(1)<<bit) != 0
}

func (s *udpSequenceObj) set(sequence uint64) {
	word, bit := sequence%throughputSequenceWindow/64, sequence%64
	s.bits[word] |= uint64(1) << bit
}

func (s *udpSequenceObj) clear(sequence uint64) {
	word, bit := sequence%throughputSequenceWindow/64, sequence%64
	s.bits[word] &^= uint64(1) << bit
}

func (r *throughputRegistryObj) create(id, transport, network string) error {
	run, err := newThroughputRun(id, transport, network, time.Now())
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pruneLocked(time.Now())
	if r.count >= maxThroughputRuns {
		return errTooManyThroughputRuns
	}
	if _, loaded := r.runs.LoadOrStore(id, run); loaded {
		return errThroughputRunExists
	}
	run.active.Store(true)
	r.count++
	return nil
}

func (r *throughputRegistryObj) lookup(id, transport, network string) *throughputRunObj {
	value, ok := r.runs.Load(id)
	if !ok {
		return nil
	}
	run := value.(*throughputRunObj)
	if run.transport != transport || run.network != network || !run.active.Load() {
		return nil
	}
	return run
}

func (r *throughputRegistryObj) finish(id string) (throughputReceiverObj, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.runs.LoadAndDelete(id)
	if !ok {
		return throughputReceiverObj{}, errThroughputRunNotFound
	}
	r.count--
	run := value.(*throughputRunObj)
	run.active.Store(false)
	return run.snapshot(), nil
}

func (r *throughputRegistryObj) pruneLocked(now time.Time) {
	r.runs.Range(func(key, value any) bool {
		run := value.(*throughputRunObj)
		if now.After(run.expiresAt) {
			r.runs.Delete(key)
			run.active.Store(false)
			r.count--
		}
		return true
	})
}
