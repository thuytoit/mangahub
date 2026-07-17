package ws

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ── Types ─────────────────────────────────────────────────────────────────────

type RoomMessage struct {
	room    string
	message []byte
}

type DirectMessage struct {
	sender    *Client
	recipient string
	message   []byte
}

// Hub manages all WebSocket clients, rooms, and message routing.
type Hub struct {
	rooms      map[string]map[*Client]bool
	usernames  map[string]*Client
	broadcast  chan *RoomMessage
	register   chan *Client
	unregister chan *Client
	directMsg  chan *DirectMessage
	mu         sync.RWMutex // protects rooms and usernames for safe reads
}

// NewHub creates a fully initialised Hub.
func NewHub() *Hub {
	return &Hub{
		rooms:      make(map[string]map[*Client]bool),
		usernames:  make(map[string]*Client),
		broadcast:  make(chan *RoomMessage, 256),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		directMsg:  make(chan *DirectMessage),
	}
}

// Run is the Hub's main event loop — call it in a goroutine.
func (h *Hub) Run() {
	for {
		select {

		case client := <-h.register:
			if _, taken := h.usernames[client.username]; taken {
				errMsg, _ := json.Marshal(map[string]string{"type": "error", "content": "username already taken: " + client.username})
				client.send <- errMsg
				close(client.send)
				continue
			}
			h.usernames[client.username] = client
			if h.rooms[client.room] == nil {
				h.rooms[client.room] = make(map[*Client]bool)
			}
			h.rooms[client.room][client] = true
			log.Printf("[WS] %s joined room #%s (%d online)", client.username, client.room, len(h.usernames))
			h.broadcastToRoom(client.room, chatMsg("join", "System", client.username+" joined the discussion"))

		case client := <-h.unregister:
			if _, ok := h.usernames[client.username]; !ok {
				continue
			}
			room := client.room
			h.dropClient(client)
			log.Printf("[WS] %s left room #%s (%d online)", client.username, room, len(h.usernames))
			h.broadcastToRoom(room, chatMsg("leave", "System", client.username+" left the discussion"))

		case rm := <-h.broadcast:
			for c := range h.rooms[rm.room] {
				select {
				case c.send <- rm.message:
				default:
					h.dropClient(c)
				}
			}

		case dm := <-h.directMsg:
			if dm.sender.username == dm.recipient {
				dm.sender.sendMsg(errMsg("cannot send DM to yourself"))
				continue
			}
			recipient, ok := h.usernames[dm.recipient]
			if !ok {
				dm.sender.sendMsg(errMsg("user not found: " + dm.recipient))
				continue
			}
			select {
			case recipient.send <- dm.message:
				dm.sender.sendMsg(sysMsg("DM sent to " + dm.recipient))
			default:
				h.dropClient(recipient)
			}
		}
	}
}

func (h *Hub) broadcastToRoom(room string, data []byte) {
	for c := range h.rooms[room] {
		select {
		case c.send <- data:
		default:
			h.dropClient(c)
		}
	}
}

func (h *Hub) dropClient(c *Client) {
	delete(h.usernames, c.username)
	if clients, ok := h.rooms[c.room]; ok {
		delete(clients, c)
		if len(clients) == 0 {
			delete(h.rooms, c.room)
		}
	}
	select {
	case <-c.send: // already closed
	default:
		close(c.send)
	}
}

// MoveToRoom safely moves a client between rooms (called from inside Run).
func (h *Hub) MoveToRoom(client *Client, newRoom string) string {
	oldRoom := client.room
	if clients, ok := h.rooms[oldRoom]; ok {
		delete(clients, client)
		if len(clients) == 0 {
			delete(h.rooms, oldRoom)
		}
	}
	h.broadcastToRoom(oldRoom, chatMsg("leave", "System", client.username+" moved to #"+newRoom))
	client.room = newRoom
	if h.rooms[newRoom] == nil {
		h.rooms[newRoom] = make(map[*Client]bool)
	}
	h.rooms[newRoom][client] = true
	h.broadcastToRoom(newRoom, chatMsg("join", "System", client.username+" joined #"+newRoom))
	return fmt.Sprintf("joined room #%s", newRoom)
}

// ListUsers returns usernames in a given room.
func (h *Hub) ListUsers(room string) []string {
	var names []string
	for c := range h.rooms[room] {
		names = append(names, c.username)
	}
	return names
}

// ListRooms returns all active rooms with user counts.
func (h *Hub) ListRooms() map[string]int {
	result := make(map[string]int)
	for room, clients := range h.rooms {
		result[room] = len(clients)
	}
	return result
}

// ConnectedCount returns total connected users.
func (h *Hub) ConnectedCount() int { return len(h.usernames) }

// ── Message helpers ───────────────────────────────────────────────────────────

func chatMsg(msgType, sender, content string) []byte {
	m := map[string]string{
		"type":      msgType,
		"sender":    sender,
		"content":   content,
		"timestamp": time.Now().Format(time.RFC3339),
	}
	data, _ := json.Marshal(m)
	return data
}

func errMsg(content string) []byte {
	data, _ := json.Marshal(map[string]string{"type": "error", "content": content})
	return data
}

func sysMsg(content string) []byte {
	data, _ := json.Marshal(map[string]string{"type": "system", "content": content})
	return data
}

// ── Client ────────────────────────────────────────────────────────────────────

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 512
)

// Client represents one WebSocket connection.
type Client struct {
	hub      *Hub
	conn     *websocket.Conn
	send     chan []byte
	username string
	room     string
}

// NewClient creates and registers a client then starts its pumps.
func NewClient(hub *Hub, conn *websocket.Conn, username, room string) {
	if username == "" {
		username = "Anonymous"
	}
	if room == "" {
		room = "general"
	}
	c := &Client{
		hub:      hub,
		conn:     conn,
		send:     make(chan []byte, 256),
		username: username,
		room:     room,
	}
	hub.register <- c
	go c.writePump()
	go c.readPump()
}

func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()
	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("[WS] Unexpected close from %s: %v", c.username, err)
			}
			break
		}

		var msg map[string]string
		if err := json.Unmarshal(raw, &msg); err != nil {
			c.sendMsg(errMsg("invalid JSON"))
			continue
		}

		content := strings.TrimSpace(msg["content"])
		msgType := msg["type"]

		switch msgType {
		case "chat":
			if content == "" {
				c.sendMsg(errMsg("content cannot be empty"))
				continue
			}
			out := map[string]string{
				"type":      "chat",
				"sender":    c.username,
				"content":   content,
				"room":      c.room,
				"timestamp": time.Now().Format(time.RFC3339),
			}
			data, _ := json.Marshal(out)
			log.Printf("[WS] [%s] %s: %s", c.room, c.username, content)
			c.hub.broadcast <- &RoomMessage{room: c.room, message: data}

		case "join_room":
			if content == "" {
				c.sendMsg(errMsg("room name required"))
				continue
			}
			confirmation := c.hub.MoveToRoom(c, content)
			c.sendMsg(sysMsg(confirmation))

		case "dm":
			recipient := msg["recipient"]
			if recipient == "" || content == "" {
				c.sendMsg(errMsg("recipient and content required for DM"))
				continue
			}
			out := map[string]string{
				"type":      "dm",
				"sender":    c.username,
				"content":   content,
				"timestamp": time.Now().Format(time.RFC3339),
			}
			data, _ := json.Marshal(out)
			c.hub.directMsg <- &DirectMessage{sender: c, recipient: recipient, message: data}

		case "list_rooms":
			rooms := c.hub.ListRooms()
			parts := []string{}
			for room, count := range rooms {
				parts = append(parts, fmt.Sprintf("#%s(%d)", room, count))
			}
			c.sendMsg(sysMsg("Active rooms: " + strings.Join(parts, ", ")))

		case "list_users":
			users := c.hub.ListUsers(c.room)
			c.sendMsg(sysMsg("Users in #"+c.room+": "+strings.Join(users, ", ")))

		default:
			c.sendMsg(errMsg("unknown message type: " + msgType))
		}
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *Client) sendMsg(data []byte) {
	select {
	case c.send <- data:
	default:
	}
}
