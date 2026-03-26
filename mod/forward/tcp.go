package forward

import (
	"context"
	"fmt"
	"io"
	"net"
	"time"
)

// // // // // // // // // //

func ProxyTCP(c1, c2 net.Conn, closeTimeout time.Duration) {
	errCh := make(chan error, 2)
	go func() { _, err := io.Copy(c1, c2); errCh <- err }()
	go func() { _, err := io.Copy(c2, c1); errCh <- err }()

	<-errCh
	_ = c1.Close()
	_ = c2.Close()

	select {
	case <-errCh:
	case <-time.After(closeTimeout):
	}
}

// //

func (m *ManagerObj) startLocalTCP(ctx context.Context) {
	for _, mapping := range m.localTCPs {
		m.wg.Add(1)
		go func(mp TCPMappingObj) {
			defer m.wg.Done()
			listener, err := net.ListenTCP("tcp", mp.Listen)
			if err != nil {
				m.log.Errorf("[forward] failed to listen on local TCP %s: %s", mp.Listen, err)
				return
			}
			defer listener.Close()
			m.log.Infof("[forward] mapping local TCP port %d to Yggdrasil %s", mp.Listen.Port, mp.Mapped)

			acceptCtx, acceptCancel := context.WithCancel(ctx)
			defer acceptCancel()
			go func() {
				<-acceptCtx.Done()
				listener.Close()
			}()

			for {
				c, err := listener.Accept()
				if err != nil {
					if ctx.Err() != nil {
						return
					}
					m.log.Errorf("[forward] local TCP accept error: %s", err)
					return
				}
				remote, err := m.node.DialContext(ctx, "tcp", fmt.Sprintf("[%s]:%d", mp.Mapped.IP, mp.Mapped.Port))
				if err != nil {
					m.log.Errorf("[forward] failed to dial %s: %s", mp.Mapped, err)
					_ = c.Close()
					continue
				}
				go ProxyTCP(c, remote, m.tcpCloseTimeout)
			}
		}(mapping)
	}
}

func (m *ManagerObj) startRemoteTCP(ctx context.Context) {
	for _, mapping := range m.remoteTCPs {
		m.wg.Add(1)
		go func(mp TCPMappingObj) {
			defer m.wg.Done()
			addr := fmt.Sprintf("[%s]:%d", m.node.Address(), mp.Listen.Port)
			listener, err := m.node.Listen("tcp", addr)
			if err != nil {
				m.log.Errorf("[forward] failed to listen on Yggdrasil TCP %s: %s", addr, err)
				return
			}
			defer listener.Close()
			m.log.Infof("[forward] mapping Yggdrasil TCP port %d to %s", mp.Listen.Port, mp.Mapped)

			acceptCtx, acceptCancel := context.WithCancel(ctx)
			defer acceptCancel()
			go func() {
				<-acceptCtx.Done()
				listener.Close()
			}()

			for {
				c, err := listener.Accept()
				if err != nil {
					if ctx.Err() != nil {
						return
					}
					m.log.Errorf("[forward] remote TCP accept error: %s", err)
					return
				}
				remote, err := net.DialTCP("tcp", nil, mp.Mapped)
				if err != nil {
					m.log.Errorf("[forward] failed to dial %s: %s", mp.Mapped, err)
					_ = c.Close()
					continue
				}
				go ProxyTCP(c, remote, m.tcpCloseTimeout)
			}
		}(mapping)
	}
}
