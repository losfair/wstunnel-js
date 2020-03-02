package main

import (
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/buffer"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv6"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/icmp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
	"gvisor.dev/gvisor/pkg/waiter"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"syscall/js"
)

var sockets sync.Map
var nextSocketID int64
var netConf *NetConf
var netStk *stack.Stack
var netDispatcher stack.NetworkDispatcher
var globalWsEndpoint GlobalWsEndpoint
var nicId tcpip.NICID = 1

var netReady = make(chan struct{})

type SocketID int64

type GlobalWsEndpoint struct {
}

func (e GlobalWsEndpoint) MTU() uint32 {
	return 1500
}

func (e GlobalWsEndpoint) Capabilities() stack.LinkEndpointCapabilities {
	return stack.CapabilityNone
}

func (e GlobalWsEndpoint) MaxHeaderLength() uint16 {
	return 0
}

func (e GlobalWsEndpoint) LinkAddress() tcpip.LinkAddress {
	return "00:00:00:00:00:00"
}

func (e GlobalWsEndpoint) WritePacket(r *stack.Route, gso *stack.GSO, protocol tcpip.NetworkProtocolNumber, pkt tcpip.PacketBuffer) *tcpip.Error {
	data := append([]byte{}, pkt.NetworkHeader...)
	data = append(data, pkt.TransportHeader...)
	data = append(data, pkt.Data.ToView()...)
	jsBuffer := js.Global().Get("Uint8Array").New(len(data))
	js.CopyBytesToJS(jsBuffer, data)
	globalWs.Call("send", jsBuffer)
	return nil
}

func (e GlobalWsEndpoint) WritePackets(r *stack.Route, gso *stack.GSO, pkts []tcpip.PacketBuffer, protocol tcpip.NetworkProtocolNumber) (int, *tcpip.Error) {
	panic("WritePackets is not implemented")
}

func (e GlobalWsEndpoint) WriteRawPacket(vv buffer.VectorisedView) *tcpip.Error {
	panic("WriteRawPacket is not implemented")
}

func (e GlobalWsEndpoint) Attach(dispatcher stack.NetworkDispatcher) {
	netDispatcher = dispatcher
}

func (e GlobalWsEndpoint) IsAttached() bool {
	return netDispatcher != nil
}

func (e GlobalWsEndpoint) Wait() {
	panic("Wait() is not implemented")
}

type Socket struct {
	endpoint   tcpip.Endpoint
	wq         waiter.Queue
	netProto   string
	transProto string

	// Buffer for received but not yet read TCP data.
	recvBufMu sync.Mutex
	recvBuf   []byte
}

func allocSocket(sock *Socket) SocketID {
	id := SocketID(atomic.AddInt64(&nextSocketID, 1))
	sockets.Store(id, sock)
	return id
}

func getSocket(id SocketID) (*Socket, bool) {
	if x, ok := sockets.Load(id); ok {
		return x.(*Socket), true
	} else {
		return nil, false
	}
}

func dropSocket(id SocketID) {
	sockets.Delete(id)
}

func dispatchPacket(pkt []byte) {
	waitForNet()

	if len(pkt) == 0 {
		return
	}

	var proto tcpip.NetworkProtocolNumber
	var rawProto = pkt[0] >> 4
	if rawProto == 4 {
		proto = ipv4.ProtocolNumber
	} else if rawProto == 6 {
		proto = ipv6.ProtocolNumber
	} else {
		return
	}

	netDispatcher.DeliverNetworkPacket(
		GlobalWsEndpoint{}, "00:00:00:00:00:00", "00:00:00:00:00:00", proto,
		tcpip.PacketBuffer{Data: buffer.NewVectorisedView(len(pkt), []buffer.View{pkt})},
	)
}

func configureNetwork(conf NetConf) {
	log.Println("IPv4 configuration:", &conf.IPv4)
	log.Println("IPv6 configuration:", &conf.IPv6)

	netConf = &conf

	netStk = stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocol{ipv4.NewProtocol(), ipv6.NewProtocol()},
		TransportProtocols: []stack.TransportProtocol{tcp.NewProtocol(), udp.NewProtocol(), icmp.NewProtocol4(), icmp.NewProtocol6()},
	})
	if err := netStk.CreateNIC(nicId, globalWsEndpoint); err != nil {
		panic(err)
	}
	if err := netStk.AddAddress(nicId, ipv4.ProtocolNumber, tcpip.Address(net.ParseIP(conf.IPv4.Address).To4())); err != nil {
		panic(err)
	}
	netStk.SetRouteTable([]tcpip.Route{
		{
			Destination: header.IPv4EmptySubnet,
			NIC:         nicId,
		},
	})

	close(netReady)
}

func (sock *Socket) ResolveAddress(addr string) (net.IP, uint16) {
	fakeTcpProto := ""
	if sock.netProto == "ip4" {
		fakeTcpProto = "tcp4"
	} else {
		fakeTcpProto = "tcp6"
	}

	resolved, err := net.ResolveTCPAddr(fakeTcpProto, addr)
	if err != nil {
		panic("ResolveAddress: invalid address")
	}

	return resolved.IP, uint16(resolved.Port)
}

func JsSocket(network string, transport string) SocketID {
	waitForNet()

	var netProto tcpip.NetworkProtocolNumber
	var transProto tcpip.TransportProtocolNumber

	switch network {
	case "ip4":
		netProto = ipv4.ProtocolNumber
	case "ip6":
		netProto = ipv6.ProtocolNumber
	default:
		panic("unknown network")
	}

	switch transport {
	case "tcp":
		transProto = tcp.ProtocolNumber
	case "udp":
		transProto = udp.ProtocolNumber
	case "icmp":
		if network == "ip4" {
			transProto = icmp.ProtocolNumber4
		} else if network == "ip6" {
			transProto = icmp.ProtocolNumber6
		}
	default:
		panic("unknown transport")
	}

	var sock = &Socket{
		netProto:   network,
		transProto: transport,
	}
	var tcpipErr *tcpip.Error

	if sock.endpoint, tcpipErr = netStk.NewEndpoint(transProto, netProto, &sock.wq); tcpipErr != nil {
		panic(tcpipErr)
	}

	return allocSocket(sock)
}

func JsConnect(sockId SocketID, remoteAddr string) {
	waitForNet()
	sock, ok := getSocket(sockId)
	if !ok {
		panic("invalid socket id")
	}

	ip, port := sock.ResolveAddress(remoteAddr)
	fullAddr := tcpip.FullAddress{
		NIC:  nicId,
		Addr: tcpip.Address(ip),
		Port: uint16(port),
	}

	waitEntry, notifyCh := waiter.NewChannelEntry(nil)

	sock.wq.EventRegister(&waitEntry, waiter.EventOut)
	terr := sock.endpoint.Connect(fullAddr)
	if terr == tcpip.ErrConnectStarted {
		<-notifyCh
		terr = sock.endpoint.GetSockOpt(tcpip.ErrorOption{})
	}
	sock.wq.EventUnregister(&waitEntry)

	if terr != nil {
		panic(terr)
	}
}

func JsClose(sockId SocketID) {
	waitForNet()
	sock, ok := getSocket(sockId)
	if !ok {
		panic("invalid socket id")
	}
	dropSocket(sockId)
	sock.endpoint.Close()
}

func readIovec(iov js.Value) []byte {
	iovCount := iov.Length()

	totalLen := 0
	for i := 0; i < iovCount; i++ {
		totalLen += iov.Index(i).Get("byteLength").Int()
	}

	payload := make([]byte, totalLen)

	index := 0
	for i := 0; i < iovCount; i++ {
		index += js.CopyBytesToGo(payload[index:], iov.Index(i))
	}
	return payload
}

func writeIovec(iov js.Value, src []byte) int {
	iovCount := iov.Length()
	index := 0
	for i := 0; i < iovCount; i++ {
		index += js.CopyBytesToJS(iov.Index(i), src[index:])
	}
	return index
}

func JsSend(sockId SocketID, jsIovec js.Value, remoteAddr string) int32 {
	waitForNet()
	sock, ok := getSocket(sockId)
	if !ok {
		panic("invalid socket id")
	}

	payload := readIovec(jsIovec)

	var dstAddr *tcpip.FullAddress
	if remoteAddr != "" {
		ip, port := sock.ResolveAddress(remoteAddr)
		dstAddr = &tcpip.FullAddress{
			NIC:  nicId,
			Addr: tcpip.Address(ip),
			Port: port,
		}
	}

	waitEntry, notifyCh := waiter.NewChannelEntry(nil)

	sock.wq.EventRegister(&waitEntry, waiter.EventOut)
	defer sock.wq.EventUnregister(&waitEntry)

	for {
		n, _, err := sock.endpoint.Write(tcpip.SlicePayload(payload), tcpip.WriteOptions{To: dstAddr})
		if err != nil {
			if err == tcpip.ErrClosedForReceive {
				return 0
			}
			if err == tcpip.ErrWouldBlock {
				<-notifyCh
				continue
			}
			panic(err)
		}
		return int32(n)
	}
}

func JsRecv(sockId SocketID, jsIovec js.Value) map[string]interface{} {
	waitForNet()
	sock, ok := getSocket(sockId)
	if !ok {
		panic("invalid socket id")
	}

	waitEntry, notifyCh := waiter.NewChannelEntry(nil)

	sock.wq.EventRegister(&waitEntry, waiter.EventIn)
	defer sock.wq.EventUnregister(&waitEntry)

	if sock.transProto == "tcp" {
		sock.recvBufMu.Lock()
		defer sock.recvBufMu.Unlock()

		arrayLen := 0
		iovCount := jsIovec.Length()
		for i := 0; i < iovCount; i++ {
			arrayLen += jsIovec.Index(i).Get("byteLength").Int()
		}

		for {
			if len(sock.recvBuf) >= arrayLen {
				break
			}
			pkt, _, err := sock.endpoint.Read(nil)
			if err != nil {
				if err == tcpip.ErrClosedForReceive {
					break
				}
				if err == tcpip.ErrWouldBlock {
					<-notifyCh
					continue
				}
				panic(err)
			}
			sock.recvBuf = append(sock.recvBuf, pkt...)
			break
		}
		copyLen := writeIovec(jsIovec, sock.recvBuf)
		copy(sock.recvBuf, sock.recvBuf[copyLen:])
		sock.recvBuf = sock.recvBuf[:len(sock.recvBuf)-copyLen]
		return map[string]interface{}{
			"len": copyLen,
		}
	} else {
		for {
			var fullAddr tcpip.FullAddress
			pkt, _, err := sock.endpoint.Read(&fullAddr)
			if err != nil {
				if err == tcpip.ErrClosedForReceive {
					return map[string]interface{}{
						"len":    0,
						"remote": nil,
					}
				}
				if err == tcpip.ErrWouldBlock {
					<-notifyCh
					continue
				}
				panic(err)
			}
			writeLen := writeIovec(jsIovec, []byte(pkt))
			return map[string]interface{}{
				"len": writeLen,
				"remote": map[string]interface{}{
					"addr": fullAddr.Addr.String(),
					"port": fullAddr.Port,
				},
			}
		}
	}

}

func waitForNet() {
	<-netReady
}
