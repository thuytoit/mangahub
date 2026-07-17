package auth

// This is a unit test file for the auth package.
// Go automatically finds and runs any file ending in _test.go when run:
//   go test ./internal/auth/
//
// Each function starting with "Test" is one test case.
// Inside each test, we call the real function and check if the result is what we expected.
// If it is not, we call t.Errorf() to report the failure.

import (
	"strings"
	"testing"
)

// TestHashPassword checks that HashPassword produces a hashed string
// and that the hash is different from the original password.
// (If someone reads the database they should not see the real password.)
func TestHashPassword(t *testing.T) {
	password := "mypassword123"

	hash, err := HashPassword(password)

	// err should be nil — meaning no error occurred
	if err != nil {
		t.Errorf("HashPassword returned an error: %v", err)
	}

	// The hash should not be empty
	if hash == "" {
		t.Error("HashPassword returned an empty string")
	}

	// The hash should NOT equal the original password
	// (if it did, the hashing would be pointless)
	if hash == password {
		t.Error("HashPassword returned the plain password — hashing did not work")
	}

	// bcrypt hashes always start with "$2a$" — a quick sanity check
	if !strings.HasPrefix(hash, "$2a$") {
		t.Errorf("Hash does not look like a bcrypt hash: %s", hash)
	}
}

// TestCheckPassword checks that CheckPassword correctly accepts the right password
// and rejects a wrong one.
func TestCheckPassword(t *testing.T) {
	password := "correctpassword"
	wrongPassword := "wrongpassword"

	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("Setup failed — could not hash password: %v", err)
	}

	// Correct password should return true
	if !CheckPassword(password, hash) {
		t.Error("CheckPassword returned false for the correct password")
	}

	// Wrong password should return false
	if CheckPassword(wrongPassword, hash) {
		t.Error("CheckPassword returned true for a wrong password — this is a security bug")
	}
}

// TestHashPasswordIsUnique checks that hashing the same password twice
// produces two DIFFERENT hashes. This is important for security —
// bcrypt adds a random "salt" each time so identical passwords don't
// produce identical hashes in the database.
func TestHashPasswordIsUnique(t *testing.T) {
	password := "samepassword"

	hash1, _ := HashPassword(password)
	hash2, _ := HashPassword(password)

	if hash1 == hash2 {
		t.Error("Two hashes of the same password are identical — salt is not working")
	}
}

// TestGenerateToken checks that GenerateToken produces a non-empty token string.
func TestGenerateToken(t *testing.T) {
	userID := "usr_abc123"
	username := "alice"
	secret := "test-secret"

	token, err := GenerateToken(userID, username, secret)

	if err != nil {
		t.Errorf("GenerateToken returned an error: %v", err)
	}

	if token == "" {
		t.Error("GenerateToken returned an empty token")
	}

	// JWT tokens always have exactly 3 parts separated by dots
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Errorf("Token does not look like a JWT — expected 3 parts, got %d", len(parts))
	}
}

// TestValidateToken checks that a token we just generated can be validated
// and returns the correct user information back.
func TestValidateToken(t *testing.T) {
	userID := "usr_abc123"
	username := "alice"
	secret := "test-secret"

	token, err := GenerateToken(userID, username, secret)
	if err != nil {
		t.Fatalf("Setup failed — could not generate token: %v", err)
	}

	claims, err := ValidateToken(token, secret)

	if err != nil {
		t.Errorf("ValidateToken returned an error for a valid token: %v", err)
	}

	// The claims should contain the same user info we put in
	if claims.UserID != userID {
		t.Errorf("Expected UserID %q, got %q", userID, claims.UserID)
	}
	if claims.Username != username {
		t.Errorf("Expected Username %q, got %q", username, claims.Username)
	}
}

// TestValidateTokenWrongSecret checks that a token signed with one secret
// is rejected when validated with a different secret.
// This protects against token forgery.
func TestValidateTokenWrongSecret(t *testing.T) {
	token, _ := GenerateToken("usr_123", "alice", "correct-secret")

	_, err := ValidateToken(token, "wrong-secret")

	if err == nil {
		t.Error("ValidateToken accepted a token with the wrong secret — this is a security bug")
	}
}

// TestValidateTokenTampered checks that if someone manually edits a token
// to change the user ID, it gets rejected.
func TestValidateTokenTampered(t *testing.T) {
	_, err := ValidateToken("this.is.fake", "any-secret")

	if err == nil {
		t.Error("ValidateToken accepted a fake/tampered token")
	}
}
