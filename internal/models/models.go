package models

import "time"

// ── Manga ─────────────────────────────────────────────────────────────────────

type Manga struct {
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	Author        string   `json:"author"`
	Genres        []string `json:"genres"`
	Status        string   `json:"status"`
	TotalChapters int      `json:"total_chapters"`
	Description   string   `json:"description"`
	CoverURL      string   `json:"cover_url"`
}

// ── User ──────────────────────────────────────────────────────────────────────

type User struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
}

// ── Progress ──────────────────────────────────────────────────────────────────

type UserProgress struct {
	UserID         string    `json:"user_id"`
	MangaID        string    `json:"manga_id"`
	CurrentChapter int       `json:"current_chapter"`
	Status         string    `json:"status"` // reading | completed | plan_to_read | on_hold | dropped
	UpdatedAt      time.Time `json:"updated_at"`
}

// ── HTTP request / response bodies ───────────────────────────────────────────

type RegisterRequest struct {
	Username string `json:"username" binding:"required,min=3,max=50"`
	Email    string `json:"email"    binding:"required,email"`
	Password string `json:"password" binding:"required,min=8"`
}

type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type LoginResponse struct {
	Token    string `json:"token"`
	Username string `json:"username"`
	UserID   string `json:"user_id"`
}

type AddLibraryRequest struct {
	MangaID string `json:"manga_id" binding:"required"`
	Status  string `json:"status"   binding:"required"`
}

type UpdateProgressRequest struct {
	MangaID string `json:"manga_id" binding:"required"`
	Chapter int    `json:"chapter"  binding:"required"`
	Status  string `json:"status"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

// ── TCP ───────────────────────────────────────────────────────────────────────

type ProgressUpdate struct {
	UserID    string `json:"user_id"`
	MangaID   string `json:"manga_id"`
	Chapter   int    `json:"chapter"`
	Timestamp int64  `json:"timestamp"`
}

// ── UDP ───────────────────────────────────────────────────────────────────────

type Notification struct {
	Type      string `json:"type"`
	MangaID   string `json:"manga_id"`
	Message   string `json:"message"`
	Timestamp int64  `json:"timestamp"`
}

// ── WebSocket ─────────────────────────────────────────────────────────────────

type ChatMessage struct {
	UserID    string `json:"user_id"`
	Username  string `json:"username"`
	Message   string `json:"message"`
	Timestamp int64  `json:"timestamp"`
	Room      string `json:"room,omitempty"`
}
