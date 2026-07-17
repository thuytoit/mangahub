package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"mangahub/internal/grpcserver"

	"github.com/gorilla/websocket"
)

// ── Config ────────────────────────────────────────────────────────────────────

const (
	httpBase = "http://localhost:8080"
	tcpAddr  = "localhost:9090"
	udpAddr  = "localhost:9091"
	wsBase   = "ws://localhost:9093"
)

// ── State ─────────────────────────────────────────────────────────────────────

var (
	token    string
	userID   string
	username string
)

// ── Main ──────────────────────────────────────────────────────────────────────

func main() {
	fmt.Println("╔══════════════════════════════════════╗")
	fmt.Println("║      MangaHub CLI Client              ║")
	fmt.Println("╚══════════════════════════════════════╝")
	fmt.Println("Type 'help' for available commands.")
	fmt.Println()

	scanner := bufio.NewScanner(os.Stdin)
	prompt()

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			prompt()
			continue
		}
		parts := strings.Fields(line)
		cmd := parts[0]
		args := parts[1:]
		handleCommand(cmd, args)
		prompt()
	}
}

func prompt() {
	if username != "" {
		fmt.Printf("[%s]> ", username)
	} else {
		fmt.Print("mangahub> ")
	}
}

func handleCommand(cmd string, args []string) {
	switch cmd {
	case "help":
		printHelp()
	case "register":
		if len(args) < 3 {
			fmt.Println("Usage: register <username> <email> <password>")
			return
		}
		register(args[0], args[1], args[2])
	case "login":
		if len(args) < 2 {
			fmt.Println("Usage: login <username> <password>")
			return
		}
		login(args[0], args[1])
	case "logout":
		token = ""
		userID = ""
		username = ""
		fmt.Println("Logged out.")
	case "search":
		if len(args) == 0 {
			fmt.Println("Usage: search <query> [--genre <genre>] [--status ongoing|completed]")
			return
		}
		query, genre, status := parseSearchArgs(args)
		searchManga(query, genre, status)
	case "info":
		if len(args) == 0 {
			fmt.Println("Usage: info <manga-id>")
			return
		}
		getManga(args[0])
	case "library":
		getLibrary()
	case "add":
		if len(args) < 2 {
			fmt.Println("Usage: add <manga-id> <status>  (status: reading|completed|plan_to_read|on_hold|dropped)")
			return
		}
		addToLibrary(args[0], args[1])
	case "progress":
		if len(args) < 2 {
			fmt.Println("Usage: progress <manga-id> <chapter>")
			return
		}
		var chapter int
		fmt.Sscanf(args[1], "%d", &chapter)
		updateProgress(args[0], chapter)
	case "sync":
		connectTCP()
	case "notify":
		registerUDP()
	case "announce":
		if len(args) < 2 {
			fmt.Println("Usage: announce <manga-id> <chapter>")
			return
		}
		announceChapter(args[0], args[1])
	case "chat":
		room := "general"
		if len(args) > 0 {
			room = args[0]
		}
		connectChat(room)
	case "grpc-get":
		if len(args) == 0 {
			fmt.Println("Usage: grpc-get <manga-id>")
			return
		}
		grpcGetManga(args[0])
	case "grpc-search":
		if len(args) == 0 {
			fmt.Println("Usage: grpc-search <query>")
			return
		}
		grpcSearchManga(strings.Join(args, " "))
	case "quit", "exit", "q":
		fmt.Println("Goodbye!")
		os.Exit(0)
	default:
		fmt.Printf("Unknown command: %s — type 'help' for available commands\n", cmd)
	}
}

func printHelp() {
	fmt.Print(`
Available Commands:

  Authentication:
    register <username> <email> <password>  Create a new account
    login <username> <password>             Login and get token
    logout                                  Clear current session

  Manga:
    search <query> [--genre <g>] [--status <s>]  Search the manga database
    info <manga-id>                               View manga details

  Library & Progress:
    library                               View your reading library
    add <manga-id> <status>               Add manga to library
    progress <manga-id> <chapter>         Update reading progress (also broadcasts via TCP)

  Network Protocols:
    sync                  Connect to TCP sync server (receive live progress updates)
    notify                Register for UDP chapter notifications
    announce <manga-id> <chapter>  Send a UDP chapter alert to all registered clients
    chat [room]           Join WebSocket chat (default room: general)
    grpc-get <manga-id>   Fetch manga details via gRPC service
    grpc-search <query>   Search manga via gRPC service

  Other:
    help      Show this help
    quit      Exit the client
`)
}

// ── HTTP helpers ──────────────────────────────────────────────────────────────

func httpGet(path string) (map[string]interface{}, error) {
	req, _ := http.NewRequest("GET", httpBase+path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	json.Unmarshal(body, &result)
	return result, nil
}

func httpPost(path string, payload interface{}) (map[string]interface{}, int, error) {
	data, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", httpBase+path, strings.NewReader(string(data)))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	json.Unmarshal(body, &result)
	return result, resp.StatusCode, nil
}

func httpPut(path string, payload interface{}) (map[string]interface{}, int, error) {
	data, _ := json.Marshal(payload)
	req, _ := http.NewRequest("PUT", httpBase+path, strings.NewReader(string(data)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	json.Unmarshal(body, &result)
	return result, resp.StatusCode, nil
}

// ── Auth commands ─────────────────────────────────────────────────────────────

func register(uname, email, pass string) {
	result, code, err := httpPost("/auth/register", map[string]string{
		"username": uname, "email": email, "password": pass,
	})
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	if code == 201 {
		fmt.Printf("Account created! User ID: %v\n", result["user_id"])
		fmt.Printf("Now login with: login %s <password>\n", uname)
	} else {
		fmt.Printf("Registration failed: %v\n", result["error"])
	}
}

func login(uname, pass string) {
	result, code, err := httpPost("/auth/login", map[string]string{
		"username": uname, "password": pass,
	})
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	if code == 200 {
		token = result["token"].(string)
		username = result["username"].(string)
		userID = result["user_id"].(string)
		fmt.Printf("Logged in as %s\n", username)
	} else {
		fmt.Printf("Login failed: %v\n", result["error"])
	}
}

// ── Manga commands ────────────────────────────────────────────────────────────

func parseSearchArgs(args []string) (query, genre, status string) {
	parts := []string{}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--genre":
			if i+1 < len(args) {
				genre = args[i+1]
				i++
			}
		case "--status":
			if i+1 < len(args) {
				status = args[i+1]
				i++
			}
		default:
			parts = append(parts, args[i])
		}
	}
	query = strings.Join(parts, " ")
	return
}

func searchManga(query, genre, status string) {
	path := fmt.Sprintf("/manga?q=%s&genre=%s&status=%s&limit=10",
		strings.ReplaceAll(query, " ", "+"), genre, status)
	result, err := httpGet(path)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	results, _ := result["results"].([]interface{})
	fmt.Printf("\nFound %v result(s) for %q:\n\n", result["total"], query)
	fmt.Printf("%-25s %-35s %-12s %-10s %s\n", "ID", "Title", "Author", "Status", "Chapters")
	fmt.Println(strings.Repeat("─", 100))
	for _, r := range results {
		m := r.(map[string]interface{})
		id := fmt.Sprintf("%v", m["id"])
		title := fmt.Sprintf("%v", m["title"])
		author := fmt.Sprintf("%v", m["author"])
		st := fmt.Sprintf("%v", m["status"])
		chapters := fmt.Sprintf("%v", m["total_chapters"])
		if len(id) > 24 {
			id = id[:24]
		}
		if len(title) > 34 {
			title = title[:34]
		}
		if len(author) > 11 {
			author = author[:11]
		}
		fmt.Printf("%-25s %-35s %-12s %-10s %s\n", id, title, author, st, chapters)
	}
	fmt.Println()
}

func getManga(id string) {
	result, err := httpGet("/manga/" + id)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	if errMsg, ok := result["error"]; ok {
		fmt.Println("Error:", errMsg)
		return
	}
	fmt.Printf("\n┌─────────────────────────────────────────┐\n")
	fmt.Printf("│  %s\n", result["title"])
	fmt.Printf("└─────────────────────────────────────────┘\n")
	fmt.Printf("  ID:       %v\n", result["id"])
	fmt.Printf("  Author:   %v\n", result["author"])
	fmt.Printf("  Status:   %v\n", result["status"])
	fmt.Printf("  Chapters: %v\n", result["total_chapters"])
	if genres, ok := result["genres"].([]interface{}); ok {
		gs := []string{}
		for _, g := range genres {
			gs = append(gs, fmt.Sprintf("%v", g))
		}
		fmt.Printf("  Genres:   %s\n", strings.Join(gs, ", "))
	}
	fmt.Printf("  Desc:     %v\n\n", result["description"])
	fmt.Printf("  Add to library:  add %v <status>\n", result["id"])
	fmt.Printf("  Update progress: progress %v <chapter>\n\n", result["id"])
}

// ── Library commands ──────────────────────────────────────────────────────────

func requireLogin() bool {
	if token == "" {
		fmt.Println("You must be logged in. Use: login <username> <password>")
		return false
	}
	return true
}

func getLibrary() {
	if !requireLogin() {
		return
	}
	result, err := httpGet("/users/library")
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	library, _ := result["library"].([]interface{})
	fmt.Printf("\nYour Library (%v entries):\n\n", result["total"])
	fmt.Printf("%-25s %-10s %-8s %s\n", "Manga ID", "Status", "Chapter", "Updated")
	fmt.Println(strings.Repeat("─", 70))
	for _, item := range library {
		p := item.(map[string]interface{})
		fmt.Printf("%-25v %-10v %-8v %v\n",
			p["manga_id"], p["status"], p["current_chapter"], p["updated_at"])
	}
	fmt.Println()
}

func addToLibrary(mangaID, status string) {
	if !requireLogin() {
		return
	}
	result, code, err := httpPost("/users/library", map[string]string{
		"manga_id": mangaID, "status": status,
	})
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	if code == 201 {
		fmt.Printf("Added %s to library with status: %s\n", mangaID, status)
	} else {
		fmt.Printf("Failed: %v\n", result["error"])
	}
}

func updateProgress(mangaID string, chapter int) {
	if !requireLogin() {
		return
	}
	result, code, err := httpPut("/users/progress", map[string]interface{}{
		"manga_id": mangaID, "chapter": chapter, "status": "reading",
	})
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	if code == 200 {
		fmt.Printf("Progress updated: %s → Chapter %d\n", mangaID, chapter)
		fmt.Println("(Broadcasting to TCP sync clients...)")
	} else {
		fmt.Printf("Failed: %v\n", result["error"])
	}
}

// ── TCP Sync ──────────────────────────────────────────────────────────────────

func connectTCP() {
	fmt.Printf("Connecting to TCP sync server at %s...\n", tcpAddr)
	conn, err := net.DialTimeout("tcp", tcpAddr, 5*time.Second)
	if err != nil {
		fmt.Println("TCP connection failed:", err)
		fmt.Println("Is the server running? Start it with: go run cmd/server/main.go")
		return
	}
	defer conn.Close()
	fmt.Println("Connected! Listening for progress updates... (Ctrl+C to stop)")
	fmt.Println()

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	go func() {
		<-interrupt
		conn.Close()
	}()

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		line := scanner.Text()
		var msg map[string]interface{}
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			fmt.Println("[TCP]", line)
			continue
		}
		msgType, _ := msg["type"].(string)
		switch msgType {
		case "connected":
			fmt.Printf("[TCP] Server: %v\n", msg["message"])
		default:
			fmt.Printf("[TCP] Update — User: %v | Manga: %v | Chapter: %v\n",
				msg["user_id"], msg["manga_id"], msg["chapter"])
		}
	}
	fmt.Println("\n[TCP] Disconnected")
}

// ── UDP Notifications ─────────────────────────────────────────────────────────

func registerUDP() {
	serverAddr, _ := net.ResolveUDPAddr("udp", "localhost:9091")
	localAddr, _ := net.ResolveUDPAddr("udp", ":0")
	conn, err := net.ListenUDP("udp", localAddr)
	if err != nil {
		fmt.Println("UDP error:", err)
		return
	}
	defer conn.Close()

	reg, _ := json.Marshal(map[string]string{"type": "register"})
	conn.WriteToUDP(reg, serverAddr)
	fmt.Println("Registered for UDP notifications. Waiting... (Ctrl+C to stop)")
	fmt.Println()

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, syscall.SIGINT)

	go func() {
		<-interrupt
		conn.Close()
	}()

	buf := make([]byte, 2048)
	for {
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			break
		}
		var msg map[string]interface{}
		json.Unmarshal(buf[:n], &msg)
		msgType, _ := msg["type"].(string)
		switch msgType {
		case "registered":
			fmt.Printf("[UDP] %v\n", msg["message"])
		case "new_chapter":
			fmt.Printf("[UDP] NEW CHAPTER ALERT: %v\n", msg["message"])
		default:
			fmt.Printf("[UDP] %s\n", string(buf[:n]))
		}
	}
	fmt.Println("\n[UDP] Disconnected")
}

// announceChapter sends a UDP packet to the server to broadcast a chapter alert
// to all registered clients. Anyone running "notify" will see the alert pop up.
func announceChapter(mangaID string, chapter string) {
	serverAddr, err := net.ResolveUDPAddr("udp", "localhost:9091")
	if err != nil {
		fmt.Println("UDP error:", err)
		return
	}
	localAddr, _ := net.ResolveUDPAddr("udp", ":0")
	conn, err := net.ListenUDP("udp", localAddr)
	if err != nil {
		fmt.Println("UDP error:", err)
		return
	}
	defer conn.Close()

	msg, _ := json.Marshal(map[string]string{
		"type":     "announce",
		"manga_id": mangaID,
		"chapter":  chapter,
	})
	conn.WriteToUDP(msg, serverAddr)
	fmt.Printf("[UDP] Sent chapter alert — Manga: %s | Chapter: %s\n", mangaID, chapter)
	fmt.Println("(All clients registered with 'notify' will receive this alert)")
}

// ── gRPC commands ─────────────────────────────────────────────────────────────

// grpcGetManga verifies the gRPC server is alive, then fetches and displays manga details.
func grpcGetManga(id string) {
	fmt.Printf("[gRPC] Calling GetManga for ID: %s\n", id)

	client, err := grpcserver.NewClient("localhost:9092")
	if err != nil {
		fmt.Println("[gRPC] Cannot reach gRPC server on :9092 — is the server running?")
		return
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.GetManga(ctx, id)
	if err != nil {
		fmt.Println("[gRPC] Error:", err)
		return
	}
	if resp.Error != "" {
		fmt.Printf("[gRPC] GetManga response: not found — %v\n", resp.Error)
		return
	}
	fmt.Println("[gRPC] GetManga response:")
	fmt.Printf("  Title:       %v\n", resp.Title)
	fmt.Printf("  Author:      %v\n", resp.Author)
	fmt.Printf("  Status:      %v\n", resp.Status)
	fmt.Printf("  Chapters:    %v\n", resp.TotalChapters)
	fmt.Printf("  Description: %v\n", resp.Description)
}

// grpcSearchManga verifies the gRPC server is alive, then searches and displays results.
func grpcSearchManga(query string) {
	fmt.Printf("[gRPC] Calling SearchManga for query: %q\n", query)

	client, err := grpcserver.NewClient("localhost:9092")
	if err != nil {
		fmt.Println("[gRPC] Cannot reach gRPC server on :9092 — is the server running?")
		return
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.SearchManga(ctx, query)
	if err != nil {
		fmt.Println("[gRPC] Error:", err)
		return
	}
	fmt.Printf("[gRPC] SearchManga response — %v result(s):\n", resp.Total)
	for i, m := range resp.Results {
		fmt.Printf("  [%d] %-30v | Author: %-15v | Status: %v\n", i+1, m.Title, m.Author, m.Status)
	}
	if len(resp.Results) == 0 {
		fmt.Println("  No results found.")
	}
}

// ── WebSocket Chat ────────────────────────────────────────────────────────────

func connectChat(room string) {
	if username == "" {
		fmt.Print("Enter a display name for chat: ")
		scanner := bufio.NewScanner(os.Stdin)
		scanner.Scan()
		username = strings.TrimSpace(scanner.Text())
		if username == "" {
			username = "Guest"
		}
	}

	url := fmt.Sprintf("%s/ws?username=%s&room=%s", wsBase, username, room)
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		fmt.Println("WebSocket connection failed:", err)
		return
	}
	defer conn.Close()

	fmt.Printf("Connected to chat room #%s as %s\n", room, username)
	fmt.Println("Type a message and press Enter. Commands: /dm <user> <msg>  /join <room>  /quit")
	fmt.Println()

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, syscall.SIGINT)
	done := make(chan struct{})

	// Read messages from server
	go func() {
		defer close(done)
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				fmt.Println("\n[Chat] Disconnected")
				return
			}
			var msg map[string]string
			if err := json.Unmarshal(raw, &msg); err != nil {
				continue
			}
			ts := msg["timestamp"]
			if len(ts) >= 19 {
				ts = ts[11:19]
			}
			switch msg["type"] {
			case "chat":
				fmt.Printf("[%s] %s: %s\n", ts, msg["sender"], msg["content"])
			case "dm":
				fmt.Printf("[%s] [DM from %s]: %s\n", ts, msg["sender"], msg["content"])
			case "join", "leave":
				fmt.Printf("*** %s\n", msg["content"])
			case "system", "user_list":
				fmt.Printf("[system] %s\n", msg["content"])
			case "error":
				fmt.Printf("[error] %s\n", msg["content"])
			}
		}
	}()

	// Send messages
	scanner := bufio.NewScanner(os.Stdin)
	for {
		select {
		case <-done:
			return
		case <-interrupt:
			conn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			return
		default:
		}

		if !scanner.Scan() {
			break
		}
		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			continue
		}
		if text == "/quit" {
			return
		}

		var msg map[string]string
		if strings.HasPrefix(text, "/dm ") {
			parts := strings.SplitN(text[4:], " ", 2)
			if len(parts) < 2 {
				fmt.Println("Usage: /dm <username> <message>")
				continue
			}
			msg = map[string]string{"type": "dm", "recipient": parts[0], "content": parts[1]}
		} else if strings.HasPrefix(text, "/join ") {
			msg = map[string]string{"type": "join_room", "content": text[6:]}
		} else if text == "/rooms" {
			msg = map[string]string{"type": "list_rooms", "content": " "}
		} else if text == "/users" {
			msg = map[string]string{"type": "list_users", "content": " "}
		} else {
			msg = map[string]string{"type": "chat", "content": text}
		}

		data, _ := json.Marshal(msg)
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			fmt.Println("Send error:", err)
			return
		}
	}
}
