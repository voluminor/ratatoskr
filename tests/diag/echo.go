package main

import (
	"context"
	"errors"
	"io"
	"net"
	"time"
)

// // // // // // // // // //

const udpEchoBufferSize = 65535

func (s *serverObj) startTCPEcho(ctx context.Context) error {
	ln, err := s.node.Listen("tcp", s.yggAddress(s.cfg.TCPEchoPort))
	if err != nil {
		return err
	}
	s.log.Infof("Yggdrasil TCP echo listening on %s", ln.Addr())
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() { _ = ln.Close() }()
		go func() {
			<-ctx.Done()
			_ = ln.Close()
		}()
		for {
			conn, err := ln.Accept()
			if err != nil {
				if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
					return
				}
				s.log.Warnf("TCP echo accept error: %v", err)
				continue
			}
			s.wg.Add(1)
			go func(c net.Conn) {
				defer s.wg.Done()
				defer func() { _ = c.Close() }()
				_ = c.SetDeadline(time.Now().Add(5 * time.Minute))
				_, _ = io.Copy(c, c)
			}(conn)
		}
	}()
	return nil
}

func (s *serverObj) startUDPEcho(ctx context.Context) error {
	pc, err := s.node.ListenPacket("udp", s.yggAddress(s.cfg.UDPEchoPort))
	if err != nil {
		return err
	}
	s.log.Infof("Yggdrasil UDP echo listening on %s", pc.LocalAddr())
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() { _ = pc.Close() }()
		go func() {
			<-ctx.Done()
			_ = pc.Close()
		}()
		buf := make([]byte, udpEchoBufferSize)
		for {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
					return
				}
				s.log.Warnf("UDP echo read error: %v", err)
				continue
			}
			if n == 0 {
				continue
			}
			if _, err = pc.WriteTo(buf[:n], addr); err != nil {
				s.log.Warnf("UDP echo write error: %v", err)
			}
		}
	}()
	return nil
}
