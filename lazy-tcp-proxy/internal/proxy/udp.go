package proxy

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/mountain-pass/lazy-tcp-proxy/internal/types"
)

const udpBufSize = 65535

const (
	udpFirstDatagramRetries  = 10
	udpFirstDatagramInterval = 500 * time.Millisecond
)

// udpFlow represents one active client→container UDP flow.
type udpFlow struct {
	clientAddr   *net.UDPAddr
	upstreamConn *net.UDPConn
	lastActive   time.Time
	connectionID string // UUID v4 correlating udp_flow_start and udp_flow_end events
}

// udpListenerState holds the shared inbound UDP socket and all active flows
// for a single UDP listen-port → container-port mapping.
type udpListenerState struct {
	listenConn  *net.UDPConn
	targetPort  int
	info        types.TargetInfo
	lastActive  time.Time
	activeFlows atomic.Int32
	idleTimeout *time.Duration // nil = use server default
	running     bool
	removed     bool
	mu          sync.Mutex
	flows       map[string]*udpFlow // key: clientAddr.String()
	pending     map[string]bool     // client addrs whose flows are being established
}

// udpReadLoop is the core datagram dispatch loop. Runs in a goroutine per listener.
func (s *ProxyServer) udpReadLoop(uls *udpListenerState) {
	buf := make([]byte, udpBufSize)
	for {
		n, clientAddr, err := uls.listenConn.ReadFromUDP(buf)
		if err != nil {
			if uls.removed {
				return
			}
			log.Printf("proxy: udp: read error on port %d: %v", uls.targetPort, err)
			return
		}

		data := make([]byte, n)
		copy(data, buf[:n])

		if ipBlocked(clientAddr.String(), uls.info) {
			log.Printf("proxy: udp: datagram from \033[36m%s\033[0m to \033[33m%s\033[0m \033[31m(blocked)\033[0m",
				clientAddr, uls.info.ContainerName)
			continue
		}

		key := clientAddr.String()

		uls.mu.Lock()
		flow, exists := uls.flows[key]
		if !exists {
			if uls.pending[key] {
				// Flow establishment already in progress; drop — UDP clients retransmit.
				uls.mu.Unlock()
				continue
			}
			uls.pending[key] = true
			uls.mu.Unlock()
			log.Printf("proxy: udp: new internal flow attempt from \033[36m%s\033[0m to \033[33m%s\033[0m (port %d)",
				clientAddr, uls.info.ContainerName, uls.targetPort)
			go s.startUDPFlow(uls, clientAddr, data)
			continue
		}
		flow.lastActive = time.Now()
		uls.lastActive = time.Now()
		conn := flow.upstreamConn
		uls.mu.Unlock()

		if _, err := conn.Write(data); err != nil {
			log.Printf("proxy: udp: write to upstream for \033[33m%s\033[0m failed: %v", uls.info.ContainerName, err)
		}
	}
}

// startUDPFlow ensures the container is running, dials the upstream UDP port,
// registers the flow, and launches the upstream read loop.
func (s *ProxyServer) startUDPFlow(uls *udpListenerState, clientAddr *net.UDPAddr, firstDatagram []byte) {
	key := clientAddr.String()
	ctx := context.Background()

	cleanup := func() {
		uls.mu.Lock()
		delete(uls.pending, key)
		uls.mu.Unlock()
	}

	_, startErr, shared := s.startGroup.Do(uls.info.ContainerID, func() (any, error) {
		return nil, s.backend.EnsureRunning(ctx, uls.info.ContainerID)
	})
	if shared {
		log.Printf("proxy: udp: joined in-flight startup for \033[33m%s\033[0m", uls.info.ContainerName)
	}
	if startErr != nil {
		log.Printf("proxy: udp: could not start container \033[33m%s\033[0m: %v", uls.info.ContainerName, startErr)
		cleanup()
		return
	}

	var hint string
	if len(uls.info.NetworkIDs) > 0 {
		hint = uls.info.NetworkIDs[0]
	}
	host, err := s.backend.GetUpstreamHost(ctx, uls.info.ContainerID, hint)
	if err != nil {
		log.Printf("proxy: udp: could not get upstream host for \033[33m%s\033[0m: %v", uls.info.ContainerName, err)
		cleanup()
		return
	}

	upstreamAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", host, uls.targetPort))
	if err != nil {
		log.Printf("proxy: udp: could not resolve upstream addr for \033[33m%s\033[0m: %v", uls.info.ContainerName, err)
		cleanup()
		return
	}
	upstreamConn, err := net.DialUDP("udp", nil, upstreamAddr)
	if err != nil {
		log.Printf("proxy: udp: could not dial upstream for \033[33m%s\033[0m: %v", uls.info.ContainerName, err)
		cleanup()
		return
	}

	connID := newConnectionID()
	flow := &udpFlow{
		clientAddr:   clientAddr,
		upstreamConn: upstreamConn,
		lastActive:   time.Now(),
		connectionID: connID,
	}

	uls.mu.Lock()
	delete(uls.pending, key)
	uls.flows[key] = flow
	uls.lastActive = time.Now()
	uls.mu.Unlock()

	if uls.info.WebhookURL != "" {
		go s.fireWebhook(uls.info.WebhookURL, "udp_flow_start", uls.info.ContainerID, uls.info.ContainerName, connID, clientAddr.IP.String(), clientAddr.Port)
	}

	// Send the first datagram with retries: the container process may not be ready
	// to handle packets immediately after EnsureRunning returns (e.g. pihole's DNS
	// daemon needs time to bind). For request/response protocols we send, wait
	// briefly for a response, and forward it; on timeout we retry.
	// udpUpstreamReadLoop is NOT started until this loop exits so there is never
	// a concurrent reader on upstreamConn.
	buf := make([]byte, udpBufSize)
	for attempt := 1; attempt <= udpFirstDatagramRetries; attempt++ {
		if _, err := upstreamConn.Write(firstDatagram); err != nil {
			log.Printf("proxy: udp: write first datagram to \033[33m%s\033[0m failed: %v", uls.info.ContainerName, err)
			break
		}
		if err := upstreamConn.SetReadDeadline(time.Now().Add(udpFirstDatagramInterval)); err != nil {
			log.Printf("proxy: udp: set deadline for \033[33m%s\033[0m failed: %v", uls.info.ContainerName, err)
			break
		}
		n, readErr := upstreamConn.Read(buf)
		if err := upstreamConn.SetReadDeadline(time.Time{}); err != nil {
			log.Printf("proxy: udp: clear deadline for \033[33m%s\033[0m failed: %v", uls.info.ContainerName, err)
		}
		if readErr == nil {
			// Service responded — forward this first reply to the client.
			if _, werr := uls.listenConn.WriteToUDP(buf[:n], clientAddr); werr != nil {
				log.Printf("proxy: udp: write initial response to \033[36m%s\033[0m failed: %v", clientAddr, werr)
			}
			uls.mu.Lock()
			flow.lastActive = time.Now()
			uls.lastActive = time.Now()
			uls.mu.Unlock()
			break
		}
		isRetryable := (func() bool {
			if netErr, ok := readErr.(net.Error); ok && netErr.Timeout() {
				return true
			}
			return errors.Is(readErr, syscall.ECONNREFUSED)
		})()
		if isRetryable {
			if attempt < udpFirstDatagramRetries {
				log.Printf("proxy: udp: upstream \033[33m%s\033[0m not ready, retrying (%d/%d)…",
					uls.info.ContainerName, attempt, udpFirstDatagramRetries)
				continue
			}
			log.Printf("proxy: udp: upstream \033[33m%s\033[0m did not respond after %d attempts; continuing",
				uls.info.ContainerName, udpFirstDatagramRetries)
			break
		}
		// Non-retryable read error (connection closed, etc.)
		log.Printf("proxy: udp: upstream read error for \033[33m%s\033[0m: %v", uls.info.ContainerName, readErr)
		break
	}

	uls.activeFlows.Add(1)
	go s.udpUpstreamReadLoop(uls, flow)
}

// udpUpstreamReadLoop reads response datagrams from the container and forwards
// them back to the originating client via the shared listen socket.
func (s *ProxyServer) udpUpstreamReadLoop(uls *udpListenerState, flow *udpFlow) {
	defer uls.activeFlows.Add(-1)
	buf := make([]byte, udpBufSize)
	for {
		n, err := flow.upstreamConn.Read(buf)
		if err != nil {
			return // conn closed by sweeper or removal
		}
		if _, err := uls.listenConn.WriteToUDP(buf[:n], flow.clientAddr); err != nil {
			log.Printf("proxy: udp: write response to client \033[36m%s\033[0m failed: %v", flow.clientAddr, err)
		}
		uls.mu.Lock()
		flow.lastActive = time.Now()
		uls.lastActive = time.Now()
		uls.mu.Unlock()
	}
}

// udpFlowSweeper prunes flows that have been idle longer than idleTimeout.
func (s *ProxyServer) udpFlowSweeper(ctx context.Context, uls *udpListenerState, tick time.Duration) {
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if uls.removed {
				return
			}
			now := time.Now()
			eff := effectiveTimeout(uls.idleTimeout, s.idleTimeout)
			uls.mu.Lock()
			for key, flow := range uls.flows {
				if now.Sub(flow.lastActive) > eff {
					log.Printf("proxy: udp: internal flow attempt \033[36m%s\033[0m -> \033[33m%s\033[0m expired",
						flow.clientAddr, uls.info.ContainerName)
					if err := flow.upstreamConn.Close(); err != nil {
						log.Printf("proxy: udp: error closing upstream conn for flow %s: %v", flow.clientAddr, err)
					}
					delete(uls.flows, key)
					if uls.info.WebhookURL != "" {
						connID := flow.connectionID
						clientAddr := flow.clientAddr
						go s.fireWebhook(uls.info.WebhookURL, "udp_flow_end", uls.info.ContainerID, uls.info.ContainerName, connID, clientAddr.IP.String(), clientAddr.Port)
					}
				}
			}
			uls.mu.Unlock()
		}
	}
}
