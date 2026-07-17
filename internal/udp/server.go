package udp

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"mangahub/internal/models"
)

// Server is a UDP server for broadcasting chapter release notifications.
type Server struct {
	port    string
	clients map[string]*net.UDPAddr
	mu      sync.RWMutex
	conn    *net.UDPConn
}

// New creates a new UDP notification server.
func New(port string) *Server {
	return &Server{
		port:    port,
		clients: make(map[string]*net.UDPAddr),
	}
}

// Start begins listening for UDP registration packets.
func (s *Server) Start() error {
	addr, err := net.ResolveUDPAddr("udp", ":"+s.port)
	if err != nil {
		return fmt.Errorf("resolve udp addr: %w", err)
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return fmt.Errorf("listen udp: %w", err)
	}
	s.conn = conn
	log.Printf("[UDP] Notification server listening on :%s", s.port)

	for {
		buf := make([]byte, 1024)
		n, clientAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			log.Printf("[UDP] Read error: %v", err)
			continue
		}
		go s.handlePacket(buf[:n], clientAddr)
	}
}

func (s *Server) handlePacket(data []byte, addr *net.UDPAddr) {
	var msg map[string]string
	if err := json.Unmarshal(data, &msg); err != nil {
		log.Printf("[UDP] Invalid packet from %s: %v", addr, err)
		return
	}

	switch msg["type"] {
	case "register":
		s.mu.Lock()
		s.clients[addr.String()] = addr
		s.mu.Unlock()
		log.Printf("[UDP] Client registered: %s (total: %d)", addr, len(s.clients))

		// Send confirmation back to the client that just registered
		ack := map[string]interface{}{
			"type":    "registered",
			"message": "You will receive chapter notifications",
			"time":    time.Now().Unix(),
		}
		ackData, _ := json.Marshal(ack)
		s.conn.WriteToUDP(ackData, addr)

	case "announce":
		// A client (or admin) is requesting a broadcast to all registered clients.
		// The packet contains the manga_id and chapter fields.
		mangaID := msg["manga_id"]
		chapter := msg["chapter"]
		if mangaID == "" {
			log.Printf("[UDP] Announce packet missing manga_id from %s", addr)
			return
		}
		log.Printf("[UDP] Announce request from %s — manga: %s chapter: %s", addr, mangaID, chapter)
		s.BroadcastNotification(&models.Notification{
			Type:    "new_chapter",
			MangaID: mangaID,
			Message: fmt.Sprintf("New chapter %s released for %s!", chapter, mangaID),
		})

	case "unsubscribe":
		s.mu.Lock()
		delete(s.clients, addr.String())
		s.mu.Unlock()
		log.Printf("[UDP] Client unregistered: %s", addr)

	case "ping":
		pong := map[string]interface{}{"type": "pong", "time": time.Now().Unix()}
		data, _ := json.Marshal(pong)
		s.conn.WriteToUDP(data, addr)
	}
}

// BroadcastNotification sends a notification to all registered clients.
func (s *Server) BroadcastNotification(n *models.Notification) {
	n.Timestamp = time.Now().Unix()
	data, err := json.Marshal(n)
	if err != nil {
		log.Printf("[UDP] Failed to marshal notification: %v", err)
		return
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	sent := 0
	for addrStr, addr := range s.clients {
		if _, err := s.conn.WriteToUDP(data, addr); err != nil {
			log.Printf("[UDP] Failed to send to %s: %v", addrStr, err)
			continue
		}
		sent++
	}
	log.Printf("[UDP] Notification sent to %d clients: %s", sent, n.Message)
}

// NewChapterAlert is a convenience method to announce a new chapter.
func (s *Server) NewChapterAlert(mangaID, mangaTitle string, chapter int) {
	s.BroadcastNotification(&models.Notification{
		Type:    "new_chapter",
		MangaID: mangaID,
		Message: fmt.Sprintf("New chapter %d released for %s!", chapter, mangaTitle),
	})
}

// ClientCount returns the number of registered UDP clients.
func (s *Server) ClientCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.clients)
}
