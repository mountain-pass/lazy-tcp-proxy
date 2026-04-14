package proxy

import (
	"context"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/mountain-pass/lazy-tcp-proxy/internal/types"
)

// ---- helpers ----

// startTCPEchoServer starts an in-process TCP echo server on a random port.
// It accepts connections and copies each connection back to itself.
// The listener is closed automatically when the test ends.
func startTCPEchoServer(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("startTCPEchoServer: %v", err)
	}
	t.Cleanup(func() { ln.Close() }) //nolint:errcheck
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close() //nolint:errcheck
				io.Copy(conn, conn) //nolint:errcheck
			}()
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

// startUDPEchoServer starts an in-process UDP echo server on a random port.
// It reads datagrams and writes each one back to the sender.
// The connection is closed automatically when the test ends.
func startUDPEchoServer(t *testing.T) int {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("startUDPEchoServer: %v", err)
	}
	t.Cleanup(func() { pc.Close() }) //nolint:errcheck
	go func() {
		buf := make([]byte, 65535)
		for {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			pc.WriteTo(buf[:n], addr) //nolint:errcheck
		}
	}()
	return pc.LocalAddr().(*net.UDPAddr).Port
}

// integrationMock is a containerBackend that always returns a fixed host and
// succeeds on EnsureRunning / StopContainer — no real Docker daemon needed.
type integrationMock struct{ host string }

func (m *integrationMock) EnsureRunning(_ context.Context, _ string) error    { return nil }
func (m *integrationMock) StopContainer(_ context.Context, _, _ string) error { return nil }
func (m *integrationMock) GetUpstreamHost(_ context.Context, _, _ string) (string, error) {
	return m.host, nil
}

// newIntegrationServer creates a ProxyServer backed by integrationMock.
// The server's context is cancelled when the test ends.
func newIntegrationServer(t *testing.T, host string) *ProxyServer {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return &ProxyServer{
		ctx:          ctx,
		targets:      make(map[int]*targetState),
		udpTargets:   make(map[int]*udpListenerState),
		nameToID:     make(map[string]string),
		idleTimeout:  5 * time.Minute,
		startTimeout: 30 * time.Second,
		pollInterval: 15 * time.Second,
		backend:      &integrationMock{host: host},
	}
}

// ---- tests ----

func TestTCPProxy_ForwardsData(t *testing.T) {
	echoPort := startTCPEchoServer(t)
	s := newIntegrationServer(t, "127.0.0.1")

	s.RegisterTarget(types.TargetInfo{
		ContainerID:   "ctr-tcp",
		ContainerName: "echo-tcp",
		Running:       true,
		Ports:         []types.PortMapping{{ListenPort: 0, TargetPort: echoPort}},
	})

	ts, ok := s.targets[0]
	if !ok {
		t.Fatal("target not registered")
	}
	proxyPort := ts.listener.Addr().(*net.TCPAddr).Port

	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", proxyPort), 5*time.Second)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close() //nolint:errcheck

	msg := []byte("hello")
	if _, err := conn.Write(msg); err != nil {
		t.Fatalf("write: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(5 * time.Second)) //nolint:errcheck
	got := make([]byte, len(msg))
	if _, err := io.ReadFull(conn, got); err != nil {
		t.Fatalf("read: %v", err)
	}

	if string(got) != string(msg) {
		t.Errorf("got %q, want %q", got, msg)
	}
}

func TestUDPProxy_ForwardsData(t *testing.T) {
	echoPort := startUDPEchoServer(t)
	s := newIntegrationServer(t, "127.0.0.1")

	s.RegisterTarget(types.TargetInfo{
		ContainerID:   "ctr-udp",
		ContainerName: "echo-udp",
		Running:       true,
		UDPPorts:      []types.PortMapping{{ListenPort: 0, TargetPort: echoPort}},
	})

	uls, ok := s.udpTargets[0]
	if !ok {
		t.Fatal("UDP target not registered")
	}
	proxyPort := uls.listenConn.LocalAddr().(*net.UDPAddr).Port

	clientConn, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: proxyPort})
	if err != nil {
		t.Fatalf("dial proxy UDP: %v", err)
	}
	defer clientConn.Close() //nolint:errcheck

	msg := []byte("hello-udp")
	if _, err := clientConn.Write(msg); err != nil {
		t.Fatalf("write: %v", err)
	}

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second)) //nolint:errcheck
	got := make([]byte, len(msg))
	n, err := clientConn.Read(got)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if string(got[:n]) != string(msg) {
		t.Errorf("got %q, want %q", got[:n], msg)
	}
}
