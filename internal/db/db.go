package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"mangahub/internal/models"

	_ "modernc.org/sqlite"
)

// DB wraps the sql.DB connection with helper methods.
type DB struct {
	conn *sql.DB
}

// New opens (or creates) the SQLite database and runs migrations.
func New(path string) (*DB, error) {
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	conn.SetMaxOpenConns(1) // SQLite is single-writer
	d := &DB{conn: conn}
	if err := d.migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return d, nil
}

func (d *DB) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id            TEXT PRIMARY KEY,
		username      TEXT UNIQUE NOT NULL,
		email         TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		created_at    TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS manga (
		id             TEXT PRIMARY KEY,
		title          TEXT NOT NULL,
		author         TEXT,
		genres         TEXT,
		status         TEXT,
		total_chapters INTEGER DEFAULT 0,
		description    TEXT,
		cover_url      TEXT
	);
	CREATE TABLE IF NOT EXISTS user_progress (
		user_id         TEXT NOT NULL,
		manga_id        TEXT NOT NULL,
		current_chapter INTEGER DEFAULT 0,
		status          TEXT DEFAULT 'plan_to_read',
		updated_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (user_id, manga_id)
	);
	CREATE INDEX IF NOT EXISTS idx_manga_title  ON manga(title);
	CREATE INDEX IF NOT EXISTS idx_manga_status ON manga(status);
	`
	_, err := d.conn.Exec(schema)
	return err
}

// Conn returns the raw *sql.DB for use in other packages if needed.
func (d *DB) Conn() *sql.DB { return d.conn }

// ── User operations ──────────────────────────────────────────────────────────

func (d *DB) CreateUser(u *models.User) error {
	_, err := d.conn.Exec(
		`INSERT INTO users (id, username, email, password_hash) VALUES (?, ?, ?, ?)`,
		u.ID, u.Username, u.Email, u.PasswordHash,
	)
	return err
}

func (d *DB) GetUserByUsername(username string) (*models.User, error) {
	row := d.conn.QueryRow(
		`SELECT id, username, email, password_hash, created_at FROM users WHERE username = ?`,
		username,
	)
	u := &models.User{}
	if err := row.Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.CreatedAt); err != nil {
		return nil, err
	}
	return u, nil
}

func (d *DB) GetUserByID(id string) (*models.User, error) {
	row := d.conn.QueryRow(
		`SELECT id, username, email, password_hash, created_at FROM users WHERE id = ?`, id,
	)
	u := &models.User{}
	if err := row.Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.CreatedAt); err != nil {
		return nil, err
	}
	return u, nil
}

// ── Manga operations ──────────────────────────────────────────────────────────

func (d *DB) InsertManga(m *models.Manga) error {
	genresJSON, _ := json.Marshal(m.Genres)
	_, err := d.conn.Exec(
		`INSERT OR REPLACE INTO manga (id, title, author, genres, status, total_chapters, description, cover_url)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.Title, m.Author, string(genresJSON), m.Status, m.TotalChapters, m.Description, m.CoverURL,
	)
	return err
}

func (d *DB) GetMangaByID(id string) (*models.Manga, error) {
	row := d.conn.QueryRow(
		`SELECT id, title, author, genres, status, total_chapters, description, cover_url
		 FROM manga WHERE id = ?`, id,
	)
	return scanManga(row)
}

func (d *DB) SearchManga(query, genre, status string, limit int) ([]*models.Manga, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	args := []interface{}{}
	conditions := []string{}

	if query != "" {
		conditions = append(conditions, "(title LIKE ? OR author LIKE ?)")
		like := "%" + query + "%"
		args = append(args, like, like)
	}
	if genre != "" {
		conditions = append(conditions, "genres LIKE ?")
		args = append(args, "%"+genre+"%")
	}
	if status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, status)
	}

	q := "SELECT id, title, author, genres, status, total_chapters, description, cover_url FROM manga"
	if len(conditions) > 0 {
		q += " WHERE " + strings.Join(conditions, " AND ")
	}
	q += " ORDER BY title LIMIT ?"
	args = append(args, limit)

	rows, err := d.conn.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []*models.Manga
	for rows.Next() {
		m, err := scanMangaRows(rows)
		if err != nil {
			continue
		}
		results = append(results, m)
	}
	return results, nil
}

func (d *DB) CountManga() (int, error) {
	row := d.conn.QueryRow("SELECT COUNT(*) FROM manga")
	var count int
	return count, row.Scan(&count)
}

func scanManga(row *sql.Row) (*models.Manga, error) {
	m := &models.Manga{}
	var genresJSON string
	if err := row.Scan(&m.ID, &m.Title, &m.Author, &genresJSON, &m.Status, &m.TotalChapters, &m.Description, &m.CoverURL); err != nil {
		return nil, err
	}
	json.Unmarshal([]byte(genresJSON), &m.Genres)
	return m, nil
}

func scanMangaRows(rows *sql.Rows) (*models.Manga, error) {
	m := &models.Manga{}
	var genresJSON string
	if err := rows.Scan(&m.ID, &m.Title, &m.Author, &genresJSON, &m.Status, &m.TotalChapters, &m.Description, &m.CoverURL); err != nil {
		return nil, err
	}
	json.Unmarshal([]byte(genresJSON), &m.Genres)
	return m, nil
}

// ── Progress operations ───────────────────────────────────────────────────────

func (d *DB) UpsertProgress(p *models.UserProgress) error {
	_, err := d.conn.Exec(
		`INSERT INTO user_progress (user_id, manga_id, current_chapter, status, updated_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(user_id, manga_id) DO UPDATE SET
		   current_chapter = excluded.current_chapter,
		   status          = excluded.status,
		   updated_at      = excluded.updated_at`,
		p.UserID, p.MangaID, p.CurrentChapter, p.Status, time.Now(),
	)
	return err
}

func (d *DB) GetLibrary(userID string) ([]*models.UserProgress, error) {
	rows, err := d.conn.Query(
		`SELECT user_id, manga_id, current_chapter, status, updated_at
		 FROM user_progress WHERE user_id = ? ORDER BY updated_at DESC`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []*models.UserProgress
	for rows.Next() {
		p := &models.UserProgress{}
		if err := rows.Scan(&p.UserID, &p.MangaID, &p.CurrentChapter, &p.Status, &p.UpdatedAt); err != nil {
			continue
		}
		list = append(list, p)
	}
	return list, nil
}

// ── Seed from JSON ────────────────────────────────────────────────────────────

// SeedFromFile loads manga.json into the database if the table is empty.
func (d *DB) SeedFromFile(path string) error {
	count, err := d.CountManga()
	if err != nil || count > 0 {
		return err // already seeded
	}

	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open seed file: %w", err)
	}
	defer f.Close()

	var mangas []*models.Manga
	if err := json.NewDecoder(f).Decode(&mangas); err != nil {
		return fmt.Errorf("decode seed file: %w", err)
	}

	tx, err := d.conn.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(
		`INSERT OR IGNORE INTO manga (id, title, author, genres, status, total_chapters, description, cover_url)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
	)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()

	for _, m := range mangas {
		genresJSON, _ := json.Marshal(m.Genres)
		if _, err := stmt.Exec(m.ID, m.Title, m.Author, string(genresJSON), m.Status, m.TotalChapters, m.Description, m.CoverURL); err != nil {
			log.Printf("seed insert %s: %v", m.ID, err)
		}
	}
	log.Printf("[DB] Seeded %d manga entries", len(mangas))
	return tx.Commit()
}
