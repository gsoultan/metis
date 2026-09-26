package impl

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// brokerProxy relays AMQP connections to a real broker, and misbehaves on cue.
//
// A broker that takes a publish and never confirms it is hard to ask a real
// one for. From Metis's side it is the same as a broker whose confirms do not
// arrive, which is what swallowConfirms makes this: every basic.ack and
// basic.nack the broker sends is dropped, and every other frame — channel
// open and close, heartbeats — is passed on, so only the confirms are missing.
//
// refuse and sever make it a broker that is down: new connections are hung up
// on at once, and the ones it has are cut.
type brokerProxy struct {
	target   string
	listener net.Listener

	swallowing atomic.Bool
	refusing   atomic.Bool

	mu    sync.Mutex
	conns map[net.Conn]struct{}
}

// AMQP 0-9-1 framing, as far as telling a confirm from anything else goes.
const (
	amqpFrameHeaderSize = 7 // type, channel, payload size
	amqpMethodFrame     = 1
	amqpBasicClass      = 60
	amqpBasicAck        = 80
	amqpBasicNack       = 120
)

// newBrokerProxy starts a proxy in front of the broker at brokerURL, and
// returns it and the URL that reaches the broker through it.
func newBrokerProxy(t *testing.T, brokerURL string) (*brokerProxy, string) {
	t.Helper()
	uri, err := amqp.ParseURI(brokerURL)
	if err != nil {
		t.Fatalf("read the broker URL: %v", err)
	}
	var config net.ListenConfig
	listener, err := config.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for the proxy: %v", err)
	}
	proxy := &brokerProxy{
		target:   net.JoinHostPort(uri.Host, strconv.Itoa(uri.Port)),
		listener: listener,
		conns:    map[net.Conn]struct{}{},
	}
	go proxy.accept()
	t.Cleanup(proxy.close)

	address, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("the proxy listens on %v, which is not a TCP address", listener.Addr())
	}
	uri.Host, uri.Port = address.IP.String(), address.Port
	return proxy, uri.String()
}

// swallowConfirms drops the broker's confirms while on is true.
func (p *brokerProxy) swallowConfirms(on bool) { p.swallowing.Store(on) }

// refuse hangs up on every new connection while on is true.
func (p *brokerProxy) refuse(on bool) { p.refusing.Store(on) }

// sever cuts every connection the proxy is relaying.
func (p *brokerProxy) sever() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for conn := range p.conns {
		_ = conn.Close()
	}
	clear(p.conns)
}

func (p *brokerProxy) accept() {
	for {
		client, err := p.listener.Accept()
		if err != nil {
			return
		}
		if p.refusing.Load() {
			_ = client.Close()
			continue
		}
		go p.relay(client)
	}
}

func (p *brokerProxy) relay(client net.Conn) {
	var dialer net.Dialer
	broker, err := dialer.DialContext(context.Background(), "tcp", p.target)
	if err != nil {
		_ = client.Close()
		return
	}
	if !p.track(client, broker) {
		return
	}
	go func() {
		_, _ = io.Copy(broker, client)
		p.hangUp(client, broker)
	}()
	p.relayFrames(broker, client)
	p.hangUp(client, broker)
}

// relayFrames passes the broker's frames to the client one at a time, leaving
// out the confirms while swallowing.
func (p *brokerProxy) relayFrames(broker, client net.Conn) {
	reader := bufio.NewReader(broker)
	for {
		header := make([]byte, amqpFrameHeaderSize)
		if _, err := io.ReadFull(reader, header); err != nil {
			return
		}
		// The payload, and the frame-end octet after it.
		frame := make([]byte, amqpFrameHeaderSize+int(binary.BigEndian.Uint32(header[3:]))+1)
		copy(frame, header)
		if _, err := io.ReadFull(reader, frame[amqpFrameHeaderSize:]); err != nil {
			return
		}
		if p.swallowing.Load() && isConfirmFrame(frame) {
			continue
		}
		if _, err := client.Write(frame); err != nil {
			return
		}
	}
}

// isConfirmFrame reports whether frame is a basic.ack or a basic.nack.
func isConfirmFrame(frame []byte) bool {
	if frame[0] != amqpMethodFrame || len(frame) < amqpFrameHeaderSize+4 {
		return false
	}
	payload := frame[amqpFrameHeaderSize:]
	class, method := binary.BigEndian.Uint16(payload), binary.BigEndian.Uint16(payload[2:])
	return class == amqpBasicClass && (method == amqpBasicAck || method == amqpBasicNack)
}

// track remembers a relayed pair so close can cut it, and reports false,
// having closed both, when the proxy is already closed.
func (p *brokerProxy) track(client, broker net.Conn) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.conns == nil {
		_ = client.Close()
		_ = broker.Close()
		return false
	}
	p.conns[client], p.conns[broker] = struct{}{}, struct{}{}
	return true
}

func (p *brokerProxy) hangUp(conns ...net.Conn) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, conn := range conns {
		_ = conn.Close()
		delete(p.conns, conn)
	}
}

func (p *brokerProxy) close() {
	_ = p.listener.Close()
	p.mu.Lock()
	defer p.mu.Unlock()
	for conn := range p.conns {
		_ = conn.Close()
	}
	p.conns = nil
}

// The proxy itself, against a stand-in broker that writes three frames: a
// heartbeat, a basic.ack and a channel.open-ok. Swallowing, only the ack is
// left out. It needs no broker, so the tests that use the proxy against one —
// which run only in CI — are not built on something that never ran here.
func TestTheBrokerProxySwallowsOnlyConfirms(t *testing.T) {
	t.Parallel()
	heartbeat := []byte{8, 0, 0, 0, 0, 0, 0, 0xCE}
	ack := []byte{amqpMethodFrame, 0, 1, 0, 0, 0, 13, 0, amqpBasicClass, 0, amqpBasicAck, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0xCE}
	openOK := []byte{amqpMethodFrame, 0, 1, 0, 0, 0, 8, 0, 20, 0, 11, 0, 0, 0, 0, 0xCE}

	var config net.ListenConfig
	standIn, err := config.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for the stand-in broker: %v", err)
	}
	t.Cleanup(func() { _ = standIn.Close() })
	go func() {
		conn, err := standIn.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		for _, frame := range [][]byte{heartbeat, ack, openOK} {
			if _, err := conn.Write(frame); err != nil {
				return
			}
		}
		_, _ = io.Copy(io.Discard, conn)
	}()

	proxy, proxied := newBrokerProxy(t, "amqp://guest:guest@"+standIn.Addr().String()+"/")
	proxy.swallowConfirms(true)
	uri, err := amqp.ParseURI(proxied)
	if err != nil {
		t.Fatalf("read the proxied URL: %v", err)
	}
	var dialer net.Dialer
	client, err := dialer.DialContext(t.Context(), "tcp", net.JoinHostPort(uri.Host, strconv.Itoa(uri.Port)))
	if err != nil {
		t.Fatalf("dial the proxy: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	want := append(append([]byte{}, heartbeat...), openOK...)
	got := make([]byte, len(want))
	if _, err := io.ReadFull(client, got); err != nil {
		t.Fatalf("read through the proxy: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("through the proxy came % x, want the heartbeat and the open-ok without the ack: % x", got, want)
	}
}

// Refusing, a dial through the proxy fails at once; severing, a connection it
// relays is cut. Neither needs a broker.
func TestTheBrokerProxyRefusesAndSevers(t *testing.T) {
	t.Parallel()
	var config net.ListenConfig
	standIn, err := config.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for the stand-in broker: %v", err)
	}
	t.Cleanup(func() { _ = standIn.Close() })
	go func() {
		for {
			conn, err := standIn.Accept()
			if err != nil {
				return
			}
			go func() { _, _ = io.Copy(io.Discard, conn) }()
		}
	}()
	proxy, proxied := newBrokerProxy(t, "amqp://guest:guest@"+standIn.Addr().String()+"/")

	proxy.refuse(true)
	if _, err := amqp.Dial(proxied); err == nil {
		t.Fatal("a dial through a refusing proxy succeeded")
	}

	proxy.refuse(false)
	uri, err := amqp.ParseURI(proxied)
	if err != nil {
		t.Fatalf("read the proxied URL: %v", err)
	}
	var dialer net.Dialer
	client, err := dialer.DialContext(t.Context(), "tcp", net.JoinHostPort(uri.Host, strconv.Itoa(uri.Port)))
	if err != nil {
		t.Fatalf("dial the proxy: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		proxy.mu.Lock()
		relaying := len(proxy.conns)
		proxy.mu.Unlock()
		if relaying > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the proxy never relayed the connection")
		}
	}
	proxy.sever()
	if err := client.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("set a read deadline: %v", err)
	}
	if _, err := client.Read(make([]byte, 1)); err == nil || errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("reading a severed connection returned %v, want it closed", err)
	}
}
