# MangaHub — Manga Tracking System

A manga tracking system built in Go that implements all five required network protocols simultaneously: HTTP REST, TCP, UDP, WebSocket, and gRPC.

---

## Table of Contents

1. [Setup Instructions](#setup-instructions)
2. [Running the Application](#running-the-application)
3. [Architecture Overview](#architecture-overview)
4. [API Documentation](#api-documentation)
5. [Protocol Demonstrations](#protocol-demonstrations)
6. [Project Structure](#project-structure)

---

## Setup Instructions

### Requirements

- Go 1.19 or later
- No other installations needed — all dependencies are managed by Go modules

### Installation

**Step 1.** Clone or unzip the project into any folder.

**Step 2.** Open a terminal and navigate into the project folder:

```bash
cd mangahub
```

**Step 3.** Download all dependencies:

```bash
go mod tidy
```

**Step 4.** (Optional) Fetch fresh manga data from MangaDex API:

```bash
go run cmd/collect/main.go
```

This saves approximately 86 manga entries to `data/manga_api.json`. The project already includes a pre-seeded `data/manga.json` with 105 entries, so this step is optional.

---

## Running the Application

### Start the Server

Open a terminal in the project folder and run:

```bash
go run cmd/server/main.go
```

All five servers start together. You will see:

```
╔══════════════════════════════════════════════╗
║        MangaHub — All servers running        ║
╠══════════════════════════════════════════════╣
║  HTTP  API   →  http://localhost:8080        ║
║  TCP   Sync  →  localhost:9090               ║
║  UDP   Notify→  localhost:9091               ║
║  gRPC  Svc   →  localhost:9092               ║
║  WS    Chat  →  ws://localhost:9093          ║
╚══════════════════════════════════════════════╝
```

### Start the Client

Open a second terminal in the same folder and run:

```bash
go run cmd/client/main.go
```

### Switching Manga Data Sources

The server seeds the database on first run. To switch data sources, delete the database file first, then restart with the desired seed:

```bash
# Use the original 105 manually curated entries (default)
del mangahub.db
go run cmd/server/main.go

# Use the live-fetched MangaDex entries
del mangahson.db
go run cmd/server/main.go --seed data/manga_api.json
```

### Run Tests

```bash
go test ./...
```

---

## Architecture Overview

MangaHub uses a single unified server process that starts all five protocol servers simultaneously. They share one SQLite database and communicate internally via Go channels.

```
┌──────────────────────────────────────────────────────┐
│                   Client (CLI)                       │
│  cmd/client/main.go                                  │
└──────┬──────┬──────┬──────────┬──────────────────────┘
       │HTTP  │TCP   │UDP       │WebSocket    │gRPC
       ▼      ▼      ▼          ▼             ▼
┌──────────────────────────────────────────────────────┐
│                  MangaHub Server                     │
│  cmd/server/main.go                                  │
│                                                      │
│  ┌──────────┐  ┌─────────┐  ┌─────────┐              │
│  │ HTTP API │  │TCP Sync │  │  UDP    │              │
│  │  :8080   │  │  :9090  │  │  :9091  │              │
│  └────┬─────┘  └────┬────┘  └─────────┘              │
│       │             │                                │
│       │ broadcast   │ on progress update             │
│       └─────────────┘                                │
│                                                      │
│  ┌──────────┐  ┌─────────┐                           │
│  │WebSocket │  │  gRPC   │                           │
│  │  :9093   │  │  :9092  │                           │
│  └──────────┘  └─────────┘                           │
│                                                      │
│              ┌──────────────┐                        │
│              │  SQLite DB   │                        │
│              │ mangahub.db  │                        │
│              └──────────────┘                        │
└──────────────────────────────────────────────────────┘
```

### How the Protocols Work Together

- When a user updates reading progress via **HTTP**, the API server immediately sends a broadcast message to the **TCP** server via an internal Go channel. All clients connected with `sync` receive the update in real time.
- **UDP** is used for chapter release announcements. Clients register their address with the UDP server, and any client (or admin) can trigger a broadcast that reaches all registered listeners instantly.
- **WebSocket** provides a full chat system with multiple rooms and direct messaging, completely independent from the other protocols.
- **gRPC** provides an internal service interface for manga queries and progress updates, demonstrating protocol-buffer-based RPC communication.

---

## API Documentation

### Base URL

```
http://localhost:8080
```

### Authentication

Protected endpoints require a JWT token in the Authorization header:

```
Authorization: Bearer <token>
```

Obtain a token by calling `POST /auth/login`.

---

### Endpoints

#### Health Check

**GET** `/health`

Returns server status and manga count. No authentication required.

Response:
```json
{
  "status": "ok",
  "manga_count": 105,
  "time": "2024-01-20T10:30:00Z"
}
```

---

#### Register

**POST** `/auth/register`

Creates a new user account.

Request body:
```json
{
  "username": "alice",
  "email": "alice@example.com",
  "password": "password123"
}
```

Validation rules:
- `username`: 3–50 characters, must be unique
- `email`: valid email format, must be unique
- `password`: minimum 8 characters

Success response (201):
```json
{
  "user_id": "usr_a1b2c3",
  "username": "alice",
  "message": "account created"
}
```

Error responses:
- `400` — missing or invalid fields
- `409` — username or email already taken

---

#### Login

**POST** `/auth/login`

Authenticates a user and returns a JWT token valid for 24 hours.

Request body:
```json
{
  "username": "alice",
  "password": "password123"
}
```

Success response (200):
```json
{
  "token": "eyJhbGci...",
  "username": "alice",
  "user_id": "usr_a1b2c3"
}
```

Error responses:
- `401` — invalid credentials

---

#### Search Manga

**GET** `/manga`

Searches the manga database. No authentication required.

Query parameters:
| Parameter | Type   | Description                        |
|-----------|--------|------------------------------------|
| `q`       | string | Search term (matches title/author) |
| `genre`   | string | Filter by genre                    |
| `status`  | string | Filter by status (ongoing/completed) |
| `limit`   | int    | Maximum results (default: 20)      |

Example: `GET /manga?q=one+piece&limit=5`

Success response (200):
```json
{
  "results": [
    {
      "id": "one-piece",
      "title": "One Piece",
      "author": "Oda Eiichiro",
      "genres": ["Action", "Adventure"],
      "status": "ongoing",
      "total_chapters": 1100,
      "description": "..."
    }
  ],
  "total": 1
}
```

---

#### Get Manga Details

**GET** `/manga/:id`

Returns full details for a specific manga by ID. No authentication required.

Example: `GET /manga/one-piece`

Success response (200): single manga object (same structure as search result)

Error responses:
- `404` — manga not found

---

#### Get Current User

**GET** `/users/me` — *Requires authentication*

Returns the logged-in user's profile.

Success response (200):
```json
{
  "user_id": "usr_a1b2c3",
  "username": "alice",
  "email": "alice@example.com"
}
```

---

#### Get Library

**GET** `/users/library` — *Requires authentication*

Returns all manga in the user's reading library.

Success response (200):
```json
{
  "library": [
    {
      "user_id": "usr_a1b2c3",
      "manga_id": "one-piece",
      "current_chapter": 1095,
      "status": "reading",
      "updated_at": "2024-01-20T15:30:00Z"
    }
  ],
  "total": 1
}
```

---

#### Add to Library

**POST** `/users/library` — *Requires authentication*

Adds a manga to the user's library.

Request body:
```json
{
  "manga_id": "one-piece",
  "status": "reading"
}
```

Valid status values: `reading`, `completed`, `plan_to_read`, `on_hold`, `dropped`

Success response (201):
```json
{
  "message": "added",
  "manga_id": "one-piece"
}
```

Error responses:
- `404` — manga ID not found in database

---

#### Update Reading Progress

**PUT** `/users/progress` — *Requires authentication*

Updates the user's current chapter for a manga. Also triggers a TCP broadcast to all connected sync clients.

Request body:
```json
{
  "manga_id": "one-piece",
  "chapter": 1095,
  "status": "reading"
}
```

Success response (200):
```json
{
  "message": "updated",
  "manga_id": "one-piece",
  "chapter": 1095
}
```

---

### TCP Sync Server — Port 9090

Connect via: `sync` command in the CLI

The TCP server accepts persistent connections and broadcasts a JSON message to all connected clients whenever any user updates their reading progress.

Broadcast message format:
```json
{
  "user_id": "usr_a1b2c3",
  "manga_id": "one-piece",
  "chapter": 1095,
  "timestamp": 1705762200
}
```

Welcome message on connection:
```json
{
  "type": "connected",
  "message": "TCP sync server connected",
  "time": 1705762200
}
```

---

### UDP Notification Server — Port 9091

Connect via: `notify` command (to register), `announce` command (to trigger)

**Register for notifications:**
Send a UDP packet to port 9091:
```json
{ "type": "register" }
```

Server responds with:
```json
{ "type": "registered", "message": "You will receive chapter notifications" }
```

**Trigger a chapter announcement:**
```json
{ "type": "announce", "manga_id": "one-piece", "chapter": "1108" }
```

All registered clients receive:
```json
{ "type": "new_chapter", "manga_id": "one-piece", "message": "New chapter 1108 released for one-piece!", "timestamp": 1705762200 }
```

---

### WebSocket Chat Server — Port 9093

Connect via: `chat [room]` command in the CLI

Connection URL format: `ws://localhost:9093/ws?username=NAME&room=ROOM`

**Send a message:**
```json
{ "type": "chat", "content": "Hello everyone!" }
```

**Send a direct message:**
```json
{ "type": "dm", "recipient": "bob", "content": "Private message" }
```

**Switch rooms:**
```json
{ "type": "join_room", "content": "naruto" }
```

**List users in current room:**
```json
{ "type": "list_users", "content": " " }
```

**List all active rooms:**
```json
{ "type": "list_rooms", "content": " " }
```

---

### gRPC Service — Port 9092

Service: `manga.MangaService`

Methods:
- `GetManga(GetMangaRequest)` — fetch a single manga by ID
- `SearchManga(SearchRequest)` — search manga with query, genre, status, limit
- `UpdateProgress(ProgressRequest)` — update user reading progress

See `proto/manga.proto` for full message definitions.

---

## Protocol Demonstrations

| CLI Command                   | Protocol  | What it demonstrates                            |
|-------------------------------|-----------|--------------------------------------------------|
| `register` / `login`          | HTTP      | JWT authentication flow                          |
| `search` / `info`             | HTTP      | GET endpoints with database queries              |
| `add` / `library`             | HTTP      | POST endpoint with database write                |
| `progress <manga> <chapter>`  | HTTP+TCP  | PUT endpoint that triggers TCP broadcast         |
| `sync`                        | TCP       | Real-time push updates to multiple clients       |
| `notify`                      | UDP       | Client registration for broadcast notifications  |
| `announce <manga> <chapter>`  | UDP       | Trigger broadcast to all registered UDP clients  |
| `chat [room]`                 | WebSocket | Real-time bidirectional messaging with rooms/DMs |
| `grpc-get <manga-id>`         | gRPC      | Unary RPC call to retrieve manga data            |
| `grpc-search <query>`         | gRPC      | Unary RPC call to search manga                   |

---

## Project Structure

```
mangahub/
├── cmd/
│   ├── server/main.go      — starts all 5 servers together
│   ├── client/main.go      — interactive CLI client
│   └── collect/main.go     — fetches manga data from MangaDex API
├── internal/
│   ├── api/server.go       — HTTP REST API (Gin framework)
│   ├── auth/auth.go        — JWT token generation and bcrypt password hashing
│   ├── db/db.go            — SQLite database operations
│   ├── grpcserver/server.go— gRPC service implementation
│   ├── models/models.go    — shared data structures
│   ├── tcp/server.go       — TCP sync server with broadcast
│   ├── udp/server.go       — UDP notification server
│   └── ws/
│       ├── hub.go          — WebSocket Hub pattern, rooms, DMs
│       └── handler.go      — WebSocket HTTP upgrade handler
├── proto/
│   └── manga.proto         — gRPC service and message definitions
├── data/
│   ├── manga.json          — 105 manually curated manga entries
│   └── manga_api.json      — entries fetched from MangaDex API
├── go.mod                  — Go module and dependency definitions
└── README.md               — this file
```

---