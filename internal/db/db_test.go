package db

// Unit tests for the database layer.
// These tests create a real but TEMPORARY database in memory (":memory:"),
// run operations against it, and then throw it away when the test ends.
// This means tests never affect the real mangahub.db file.
//
// Run these tests with:
//   go test ./internal/db/

import (
	"testing"

	"mangahub/internal/models"
)

// newTestDB is a helper that creates a fresh in-memory database for each test.
// ":memory:" is a special SQLite path that means "keep everything in RAM,
// delete it when we are done." Ideal for testing.
func newTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("Could not create test database: %v", err)
	}
	return db
}

// ── User tests ────────────────────────────────────────────────────────────────

// TestCreateUser checks that we can insert a new user into the database.
func TestCreateUser(t *testing.T) {
	db := newTestDB(t)

	user := &models.User{
		ID:           "usr_test001",
		Username:     "testuser",
		Email:        "test@example.com",
		PasswordHash: "fakehash",
	}

	err := db.CreateUser(user)
	if err != nil {
		t.Errorf("CreateUser returned an error: %v", err)
	}
}

// TestCreateUserDuplicateUsername checks that inserting two users with the
// same username fails — usernames must be unique.
func TestCreateUserDuplicateUsername(t *testing.T) {
	db := newTestDB(t)

	user1 := &models.User{ID: "usr_001", Username: "alice", Email: "alice@test.com", PasswordHash: "hash"}
	user2 := &models.User{ID: "usr_002", Username: "alice", Email: "other@test.com", PasswordHash: "hash"}

	db.CreateUser(user1)
	err := db.CreateUser(user2)

	// This SHOULD fail — duplicate username
	if err == nil {
		t.Error("CreateUser allowed a duplicate username — uniqueness constraint is not working")
	}
}

// TestGetUserByUsername checks that we can retrieve a user we just created,
// and that the returned data matches what we put in.
func TestGetUserByUsername(t *testing.T) {
	db := newTestDB(t)

	original := &models.User{
		ID:           "usr_001",
		Username:     "alice",
		Email:        "alice@test.com",
		PasswordHash: "somehash",
	}
	db.CreateUser(original)

	found, err := db.GetUserByUsername("alice")
	if err != nil {
		t.Errorf("GetUserByUsername returned an error: %v", err)
	}

	if found.ID != original.ID {
		t.Errorf("Expected ID %q, got %q", original.ID, found.ID)
	}
	if found.Email != original.Email {
		t.Errorf("Expected Email %q, got %q", original.Email, found.Email)
	}
}

// TestGetUserByUsernameNotFound checks that looking up a username that does
// not exist returns an error rather than an empty/nil result.
func TestGetUserByUsernameNotFound(t *testing.T) {
	db := newTestDB(t)

	_, err := db.GetUserByUsername("nobody")
	if err == nil {
		t.Error("GetUserByUsername should return an error for a non-existent user, but returned nil")
	}
}

// ── Manga tests ───────────────────────────────────────────────────────────────

// TestInsertAndGetManga checks that we can save a manga and retrieve it by ID.
func TestInsertAndGetManga(t *testing.T) {
	db := newTestDB(t)

	manga := &models.Manga{
		ID:            "one-piece",
		Title:         "One Piece",
		Author:        "Oda Eiichiro",
		Genres:        []string{"Action", "Adventure"},
		Status:        "ongoing",
		TotalChapters: 1100,
		Description:   "Pirates!",
	}

	err := db.InsertManga(manga)
	if err != nil {
		t.Errorf("InsertManga returned an error: %v", err)
	}

	found, err := db.GetMangaByID("one-piece")
	if err != nil {
		t.Errorf("GetMangaByID returned an error: %v", err)
	}

	if found.Title != manga.Title {
		t.Errorf("Expected title %q, got %q", manga.Title, found.Title)
	}
	if found.Author != manga.Author {
		t.Errorf("Expected author %q, got %q", manga.Author, found.Author)
	}
	if len(found.Genres) != 2 {
		t.Errorf("Expected 2 genres, got %d", len(found.Genres))
	}
}

// TestGetMangaByIDNotFound checks that requesting a manga ID that does not
// exist returns an error.
func TestGetMangaByIDNotFound(t *testing.T) {
	db := newTestDB(t)

	_, err := db.GetMangaByID("does-not-exist")
	if err == nil {
		t.Error("GetMangaByID should return an error for a missing manga")
	}
}

// TestSearchMangaByTitle checks that the search function finds manga
// whose title contains the search query.
func TestSearchMangaByTitle(t *testing.T) {
	db := newTestDB(t)

	// Insert two manga
	db.InsertManga(&models.Manga{ID: "one-piece", Title: "One Piece", Author: "Oda", Genres: []string{"Action"}, Status: "ongoing"})
	db.InsertManga(&models.Manga{ID: "naruto", Title: "Naruto", Author: "Kishimoto", Genres: []string{"Action"}, Status: "completed"})

	// Search for "naruto" — should only return 1 result
	results, err := db.SearchManga("naruto", "", "", 10)
	if err != nil {
		t.Errorf("SearchManga returned an error: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("Expected 1 result for 'naruto', got %d", len(results))
	}
	if results[0].ID != "naruto" {
		t.Errorf("Expected result ID 'naruto', got %q", results[0].ID)
	}
}

// TestSearchMangaByStatus checks that filtering by status works.
func TestSearchMangaByStatus(t *testing.T) {
	db := newTestDB(t)

	db.InsertManga(&models.Manga{ID: "one-piece", Title: "One Piece", Author: "Oda", Genres: []string{"Action"}, Status: "ongoing"})
	db.InsertManga(&models.Manga{ID: "naruto", Title: "Naruto", Author: "Kishimoto", Genres: []string{"Action"}, Status: "completed"})

	// Filter by status "completed" — should return only Naruto
	results, err := db.SearchManga("", "", "completed", 10)
	if err != nil {
		t.Errorf("SearchManga returned an error: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("Expected 1 completed manga, got %d", len(results))
	}
	if results[0].ID != "naruto" {
		t.Errorf("Expected 'naruto', got %q", results[0].ID)
	}
}

// TestSearchMangaEmpty checks that an empty query returns all manga.
func TestSearchMangaEmpty(t *testing.T) {
	db := newTestDB(t)

	db.InsertManga(&models.Manga{ID: "m1", Title: "Alpha", Author: "A", Genres: []string{"Action"}, Status: "ongoing"})
	db.InsertManga(&models.Manga{ID: "m2", Title: "Beta", Author: "B", Genres: []string{"Romance"}, Status: "completed"})

	results, err := db.SearchManga("", "", "", 10)
	if err != nil {
		t.Errorf("SearchManga returned an error: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("Expected 2 results for empty search, got %d", len(results))
	}
}

// ── Progress tests ────────────────────────────────────────────────────────────

// TestUpsertAndGetProgress checks that saving reading progress works,
// and that updating it (upsert) correctly overwrites the old value.
func TestUpsertAndGetProgress(t *testing.T) {
	db := newTestDB(t)

	// Need a user and manga in the database first
	db.CreateUser(&models.User{ID: "usr_001", Username: "alice", Email: "a@b.com", PasswordHash: "h"})
	db.InsertManga(&models.Manga{ID: "one-piece", Title: "One Piece", Author: "Oda", Genres: []string{}, Status: "ongoing"})

	// Save initial progress
	err := db.UpsertProgress(&models.UserProgress{
		UserID:         "usr_001",
		MangaID:        "one-piece",
		CurrentChapter: 50,
		Status:         "reading",
	})
	if err != nil {
		t.Errorf("UpsertProgress returned an error: %v", err)
	}

	// Update progress to a higher chapter
	err = db.UpsertProgress(&models.UserProgress{
		UserID:         "usr_001",
		MangaID:        "one-piece",
		CurrentChapter: 100,
		Status:         "reading",
	})
	if err != nil {
		t.Errorf("UpsertProgress update returned an error: %v", err)
	}

	// Retrieve library and check the chapter is now 100
	library, err := db.GetLibrary("usr_001")
	if err != nil {
		t.Errorf("GetLibrary returned an error: %v", err)
	}
	if len(library) != 1 {
		t.Errorf("Expected 1 library entry, got %d", len(library))
	}
	if library[0].CurrentChapter != 100 {
		t.Errorf("Expected chapter 100 after update, got %d", library[0].CurrentChapter)
	}
}

// TestCountManga checks that CountManga returns the correct number after inserts.
func TestCountManga(t *testing.T) {
	db := newTestDB(t)

	count, _ := db.CountManga()
	if count != 0 {
		t.Errorf("Expected 0 manga in fresh database, got %d", count)
	}

	db.InsertManga(&models.Manga{ID: "m1", Title: "A", Author: "A", Genres: []string{}, Status: "ongoing"})
	db.InsertManga(&models.Manga{ID: "m2", Title: "B", Author: "B", Genres: []string{}, Status: "ongoing"})

	count, _ = db.CountManga()
	if count != 2 {
		t.Errorf("Expected 2 manga after two inserts, got %d", count)
	}
}
