package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

// // // // // // // // // //

const throughputTCPReadBufferSize = 256 * 1024

type throughputTCPStartObj struct {
	listener  net.Listener
	transport string
}

type throughputUDPStartObj struct {
	conn      net.PacketConn
	transport string
}

func closeThroughputStarts(tcp []throughputTCPStartObj, udp []throughputUDPStartObj) {
	for _, start := range tcp {
		_ = start.listener.Close()
	}
	for _, start := range udp {
		_ = start.conn.Close()
	}
}

// //

func (s *serverObj) startThroughputSinks(ctx context.Context) error {
	var tcpStarts []throughputTCPStartObj
	var udpStarts []throughputUDPStartObj
	prepared := false
	defer func() {
		if !prepared {
			closeThroughputStarts(tcpStarts, udpStarts)
		}
	}()

	directTCP, err := net.Listen("tcp", fmt.Sprintf(":%d", s.cfg.TCPThroughputPort))
	if err != nil {
		return fmt.Errorf("listen direct TCP throughput sink: %w", err)
	}
	tcpStarts = append(tcpStarts, throughputTCPStartObj{listener: directTCP, transport: "direct"})

	yggTCP, err := s.node.Listen("tcp", s.yggAddress(s.cfg.TCPThroughputPort))
	if err != nil {
		return fmt.Errorf("listen Yggdrasil TCP throughput sink: %w", err)
	}
	tcpStarts = append(tcpStarts, throughputTCPStartObj{listener: yggTCP, transport: "ygg"})

	directUDP, err := net.ListenPacket("udp", fmt.Sprintf(":%d", s.cfg.UDPThroughputPort))
	if err != nil {
		return fmt.Errorf("listen direct UDP throughput sink: %w", err)
	}
	udpStarts = append(udpStarts, throughputUDPStartObj{conn: directUDP, transport: "direct"})

	yggUDP, err := s.node.ListenPacket("udp", s.yggAddress(s.cfg.UDPThroughputPort))
	if err != nil {
		return fmt.Errorf("listen Yggdrasil UDP throughput sink: %w", err)
	}
	udpStarts = append(udpStarts, throughputUDPStartObj{conn: yggUDP, transport: "ygg"})
	prepared = true

	for _, start := range tcpStarts {
		s.startThroughputTCP(ctx, start)
	}
	for _, start := range udpStarts {
		s.startThroughputUDP(ctx, start)
	}
	return nil
}

func (s *serverObj) startThroughputTCP(ctx context.Context, start throughputTCPStartObj) {
	s.log.Infof("%s TCP throughput sink listening on %s", start.transport, start.listener.Addr())
	s.wg.Add(2)
	go func() {
		defer s.wg.Done()
		<-ctx.Done()
		_ = start.listener.Close()
	}()
	go func() {
		defer s.wg.Done()
		defer func() { _ = start.listener.Close() }()
		for {
			conn, err := start.listener.Accept()
			if err != nil {
				if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
					return
				}
				s.log.Warnf("%s TCP throughput accept error: %v", start.transport, err)
				continue
			}
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				s.handleThroughputTCP(conn, start.transport)
			}()
		}
	}()
}

func (s *serverObj) handleThroughputTCP(conn net.Conn, transport string) {
	defer func() { _ = conn.Close() }()
	_ = conn.SetReadDeadline(time.Now().Add(throughputRunTTL))
	headerBytes := make([]byte, throughputTCPHeaderSize)
	if _, err := io.ReadFull(conn, headerBytes); err != nil {
		return
	}
	header, err := decodeThroughputHeader(headerBytes, false)
	if err != nil {
		return
	}
	run := s.throughput.lookup(header.RunID, transport, "tcp")
	if run == nil {
		return
	}
	run.tcpConns.Add(1)
	buffer := make([]byte, throughputTCPReadBufferSize)
	received, _ := io.CopyBuffer(io.Discard, conn, buffer)
	if received > 0 {
		run.receivedBytes.Add(uint64(received))
	}
}

func (s *serverObj) startThroughputUDP(ctx context.Context, start throughputUDPStartObj) {
	s.log.Infof("%s UDP throughput sink listening on %s", start.transport, start.conn.LocalAddr())
	s.wg.Add(2)
	go func() {
		defer s.wg.Done()
		<-ctx.Done()
		_ = start.conn.Close()
	}()
	go func() {
		defer s.wg.Done()
		defer func() { _ = start.conn.Close() }()
		buffer := make([]byte, udpEchoBufferSize)
		var cachedID string
		var cachedRun *throughputRunObj
		for {
			n, _, err := start.conn.ReadFrom(buffer)
			if err != nil {
				if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
					return
				}
				s.log.Warnf("%s UDP throughput read error: %v", start.transport, err)
				continue
			}
			if n <= throughputUDPHeaderSize {
				continue
			}
			header, err := decodeThroughputHeader(buffer[:n], true)
			if err != nil {
				continue
			}
			run := cachedRun
			if header.RunID != cachedID {
				cachedID = header.RunID
				run = s.throughput.lookup(header.RunID, start.transport, "udp")
				cachedRun = run
			}
			if run != nil {
				run.recordUDP(header.StreamID, header.Sequence, n-throughputUDPHeaderSize)
			}
		}
	}()
}
