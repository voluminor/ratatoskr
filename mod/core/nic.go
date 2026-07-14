package core

import (
	"net"
	"sync"
	"sync/atomic"

	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
	"github.com/yggdrasil-network/yggdrasil-go/src/ipv6rwc"

	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv6"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
)

// // // // // // // // // //

var _ stack.LinkEndpoint = (*nicObj)(nil)

var writeBufPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, 65535)
		return &buf
	},
}

// nicObj — bridge between gVisor and Yggdrasil at the IPv6 packet level
type nicObj struct {
	ns         *netstackObj
	ipv6rwc    *ipv6rwc.ReadWriteCloser
	dispatcher atomic.Pointer[stack.NetworkDispatcher]
	mtu        atomic.Uint32
	done       chan struct{}
	readDone   chan struct{}
	closeOnce  sync.Once
	logger     yggcore.Logger
}

func (s *netstackObj) newNIC(ygg *yggcore.Core, ifMTU uint64) (*nicObj, tcpip.Error) {
	rwc := ipv6rwc.NewReadWriteCloser(ygg)
	mtu := normalizeMTU(ifMTU, rwc.MaxMTU())
	rwc.SetMTU(mtu)
	nic := &nicObj{
		ns:       s,
		ipv6rwc:  rwc,
		done:     make(chan struct{}),
		readDone: make(chan struct{}),
		logger:   s.logger,
	}
	nic.mtu.Store(uint32(mtu))
	if err := s.stack.CreateNIC(1, nic); err != nil {
		return nil, err
	}

	// Read packets from Yggdrasil → deliver to netstack
	go func() {
		defer close(nic.readDone)
		readBuf := make([]byte, nic.ipv6rwc.MaxMTU())
		for {
			rx, err := nic.ipv6rwc.Read(readBuf)
			if err != nil {
				select {
				case <-nic.done:
				default:
					nic.logger.Warnf("[core] ipv6rwc read error: %v", err)
				}
				return
			}
			pkb := stack.NewPacketBuffer(stack.PacketBufferOptions{
				Payload: buffer.MakeWithData(readBuf[:rx]),
			})
			if d := nic.dispatcher.Load(); d != nil {
				(*d).DeliverNetworkPacket(ipv6.ProtocolNumber, pkb)
			}
			pkb.DecRef()
		}
	}()

	// Route for Yggdrasil subnet 0200::/7
	_, snet, err := net.ParseCIDR("0200::/7")
	if err != nil {
		nic.Close()
		return nil, &tcpip.ErrBadAddress{}
	}
	subnet, err := tcpip.NewSubnet(
		tcpip.AddrFromSlice(snet.IP.To16()),
		tcpip.MaskFrom(string(snet.Mask)),
	)
	if err != nil {
		nic.Close()
		return nil, &tcpip.ErrBadAddress{}
	}
	s.stack.AddRoute(tcpip.Route{Destination: subnet, NIC: 1})

	// Register the local address (HandleLocal is always enabled)
	ip := ygg.Address()
	if err := s.stack.AddProtocolAddress(
		1,
		tcpip.ProtocolAddress{
			Protocol:          ipv6.ProtocolNumber,
			AddressWithPrefix: tcpip.AddrFromSlice(ip.To16()).WithPrefix(),
		},
		stack.AddressProperties{},
	); err != nil {
		nic.Close()
		return nil, err
	}

	return nic, nil
}

// //

func (e *nicObj) Attach(dispatcher stack.NetworkDispatcher) {
	if dispatcher == nil {
		e.dispatcher.Store(nil)
		return
	}
	e.dispatcher.Store(&dispatcher)
}

func (e *nicObj) IsAttached() bool {
	return e.dispatcher.Load() != nil
}

func (e *nicObj) MTU() uint32 { return e.mtu.Load() }
func (e *nicObj) SetMTU(mtu uint32) {
	next := normalizeMTU(uint64(mtu), e.ipv6rwc.MaxMTU())
	e.ipv6rwc.SetMTU(next)
	e.mtu.Store(uint32(next))
}
func (*nicObj) Capabilities() stack.LinkEndpointCapabilities { return stack.CapabilityNone }
func (*nicObj) MaxHeaderLength() uint16                      { return 40 }
func (*nicObj) LinkAddress() tcpip.LinkAddress               { return "" }
func (*nicObj) SetLinkAddress(tcpip.LinkAddress)             {}
func (*nicObj) Wait()                                        {}

// //

func (e *nicObj) writePacket(pkt *stack.PacketBuffer) tcpip.Error {
	vl, offset := pkt.AsViewList()
	front := vl.Front()
	// Fast path: single View — send without copying
	if front != nil && front.Next() == nil {
		if _, err := e.ipv6rwc.Write(front.AsSlice()[offset:]); err != nil {
			return &tcpip.ErrAborted{}
		}
		return nil
	}
	// Multiple Views — assemble into a pool buffer
	bufPtr := writeBufPool.Get().(*[]byte)
	defer writeBufPool.Put(bufPtr)
	buf := *bufPtr
	n := 0
	first := true
	for v := front; v != nil; v = v.Next() {
		s := v.AsSlice()
		if first {
			s = s[offset:]
			first = false
		}
		n += copy(buf[n:], s)
	}
	_, err := e.ipv6rwc.Write(buf[:n])
	if err != nil {
		return &tcpip.ErrAborted{}
	}
	return nil
}

func (e *nicObj) WritePackets(list stack.PacketBufferList) (int, tcpip.Error) {
	for i, pkt := range list.AsSlice() {
		if err := e.writePacket(pkt); err != nil {
			return i, err
		}
	}
	return list.Len(), nil
}

// //

func (*nicObj) ARPHardwareType() header.ARPHardwareType { return header.ARPHardwareNone }
func (*nicObj) AddHeader(*stack.PacketBuffer)           {}
func (*nicObj) ParseHeader(*stack.PacketBuffer) bool    { return true }

func (e *nicObj) Close() {
	e.closeOnce.Do(func() {
		close(e.done)
		_ = e.ipv6rwc.Close()
		<-e.readDone
	})
}

func (e *nicObj) SetOnCloseAction(func()) {}
