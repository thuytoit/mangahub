package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"mangahub/internal/api"
	"mangahub/internal/db"
	"mangahub/internal/grpcserver"
	"mangahub/internal/tcp"
	"mangahub/internal/udp"
	"mangahub/internal/ws"
)

func main() {
	// ── Flags ─────────────────────────────────────────────────────────────────
	dbPath := flag.String("db", "mangahub.db", "SQLite database path")
	seedPath := flag.String("seed", "data/manga.json", "Manga seed JSON file")
	jwtSecret := flag.String("secret", "mangahub-secret-change-me", "JWT signing secret")
	httpPort := flag.String("http", ":8080", "HTTP API address")
	tcpPort := flag.String("tcp", "9090", "TCP sync port")
	udpPort := flag.String("udp", "9091", "UDP notification port")
	grpcPort := flag.String("grpc", "9092", "gRPC service port")
	wsPort := flag.String("ws", ":9093", "WebSocket chat address")
	flag.Parse()

	if envSecret := os.Getenv("JWT_SECRET"); envSecret != "" {
		*jwtSecret = envSecret
	}

	log.Println("=== MangaHub Server Starting ===")

	// ── 1. Database ───────────────────────────────────────────────────────────
	log.Println("[1/5] Initialising database...")
	database, err := db.New(*dbPath)
	if err != nil {
		log.Fatalf("Database init failed: %v", err)
	}
	if err := database.SeedFromFile(*seedPath); err != nil {
		log.Printf("Seed warning: %v", err)
	}
	count, _ := database.CountManga()
	log.Printf("      Database ready: %d manga entries", count)

	// ── 2. TCP Sync Server ────────────────────────────────────────────────────
	log.Println("[2/5] Starting TCP sync server...")
	tcpServer := tcp.New(*tcpPort)
	go func() {
		if err := tcpServer.Start(); err != nil {
			log.Printf("[TCP] Error: %v", err)
		}
	}()
	time.Sleep(50 * time.Millisecond) // give it a moment to bind

	// ── 3. UDP Notification Server ────────────────────────────────────────────
	log.Println("[3/5] Starting UDP notification server...")
	udpServer := udp.New(*udpPort)
	go func() {
		if err := udpServer.Start(); err != nil {
			log.Printf("[UDP] Error: %v", err)
		}
	}()

	// ── 4. WebSocket Chat Server ──────────────────────────────────────────────
	log.Println("[4/5] Starting WebSocket chat server...")
	chatHub := ws.NewHub()
	go chatHub.Run()

	// WebSocket HTTP server on its own port
	wsMux := http.NewServeMux()
	wsMux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		ws.ServeWS(chatHub, w, r)
	})
	wsMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("MangaHub WebSocket Chat — connect to /ws?username=NAME&room=ROOM\n"))
	})
	wsServer := &http.Server{Addr: *wsPort, Handler: wsMux}
	go func() {
		log.Printf("[WS]   Chat server on %s", *wsPort)
		if err := wsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[WS] Error: %v", err)
		}
	}()

	// ── 5. HTTP API Server ────────────────────────────────────────────────────
	log.Println("[5/5] Starting HTTP API server...")
	apiServer := api.New(database, *jwtSecret, tcpServer)
	go func() {
		if err := apiServer.Run(*httpPort); err != nil {
			log.Printf("[HTTP] Error: %v", err)
		}
	}()

	// ── gRPC Service ──────────────────────────────────────────────────────────
	grpcSrv := grpcserver.New(*grpcPort, database)
	go func() {
		if err := grpcSrv.Start(); err != nil {
			log.Printf("[gRPC] Error: %v", err)
		}
	}()

	// ── Ready banner ──────────────────────────────────────────────────────────
	time.Sleep(200 * time.Millisecond)
	fmt.Println("")
	fmt.Println("╔══════════════════════════════════════════════╗")
	fmt.Println("║        MangaHub — All servers running        ║")
	fmt.Println("╠══════════════════════════════════════════════╣")
	fmt.Println("║  HTTP  API   →  http://localhost:8080        ║")
	fmt.Println("║  TCP   Sync  →  localhost:9090               ║")
	fmt.Println("║  UDP   Notify→  localhost:9091               ║")
	fmt.Println("║  gRPC  Svc   →  localhost:9092               ║")
	fmt.Println("║  WS    Chat  →  ws://localhost:9093          ║")
	fmt.Println("╚══════════════════════════════════════════════╝")
	fmt.Println("")
	fmt.Println("Try: curl http://localhost:8080/health")
	fmt.Println("Try: curl http://localhost:8080/manga?q=one+piece")

	// ── Graceful shutdown ─────────────────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("\nShutting down gracefully...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	wsServer.Shutdown(ctx)
	grpcSrv.Stop()
	log.Println("All servers stopped. Goodbye!")
}

func padRight(s string, n int) string {
	for len(s) < n {
		s += " "
	}
	return s
}
