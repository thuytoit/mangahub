package tcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"sync"
	"time"
)

// Server is a TCP server that broadcasts progress updates to all connected clients.
type Server struct {
	port        string
	connections map[string]net.Conn
	mu          sync.RWMutex
	broadcast   chan []byte
}

// New creates a new TCP sync server.
func New(port string) *Server {
	return &Server{
		port:        port,
		connections: make(map[string]net.Conn),
		broadcast:   make(chan []byte, 256),
	}
}

// Start listens for TCP connections and starts the broadcast loop.
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", ":"+s.port)
	if err != nil {
		return fmt.Errorf("tcp listen: %w", err)
	}
	log.Printf("[TCP] Sync server listening on :%s", s.port)

	go s.broadcastLoop()

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("[TCP] Accept error: %v", err)
			continue
		}
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	addr := conn.RemoteAddr().String()
	log.Printf("[TCP] Client connected: %s", addr)

	s.mu.Lock()
	s.connections[addr] = conn
	s.mu.Unlock()

	// Send welcome message
	welcome := map[string]interface{}{
		"type":    "connected",
		"message": "TCP sync server connected",
		"time":    time.Now().Unix(),
	}
	data, _ := json.Marshal(welcome)
	conn.Write(append(data, '\n'))

	defer func() {
		s.mu.Lock()
		delete(s.connections, addr)
		s.mu.Unlock()
		conn.Close()
		log.Printf("[TCP] Client disconnected: %s (total: %d)", addr, len(s.connections))
	}()

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		// Echo back any message the client sends (heartbeat / ack)
		var msg map[string]interface{}
		if err := json.Unmarshal(line, &msg); err == nil {
			if t, ok := msg["type"].(string); ok && t == "heartbeat" {
				pong := map[string]interface{}{"type": "pong", "time": time.Now().Unix()}
				data, _ := json.Marshal(pong)
				conn.Write(append(data, '\n'))
			}
		}
	}
}

// broadcastLoop sends messages from the channel to all connected clients.
func (s *Server) broadcastLoop() {
	for msg := range s.broadcast {
		s.mu.RLock()
		for addr, conn := range s.connections {
			conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if _, err := conn.Write(append(msg, '\n')); err != nil {
				log.Printf("[TCP] Failed to write to %s: %v", addr, err)
				conn.Close()
			}
		}
		s.mu.RUnlock()
	}
}

// Broadcast queues a message to be sent to all connected clients.
func (s *Server) Broadcast(data []byte) {
	select {
	case s.broadcast <- data:
	default:
		log.Println("[TCP] Broadcast channel full, dropping message")
	}
}

// ConnectedCount returns the number of currently connected clients.
func (s *Server) ConnectedCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.connections)
}
