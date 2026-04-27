package migrations

import "testing"

func TestRedactDatabaseURL(t *testing.T) {
	got := RedactDatabaseURL("postgres://user:pass@localhost:5432/db?sslmode=disable")
	if got == "postgres://user:pass@localhost:5432/db?sslmode=disable" {
		t.Fatalf("expected password to be redacted, got %q", got)
	}
	if got != "postgres://user@localhost:5432/db?sslmode=disable" {
		t.Fatalf("unexpected redacted url: %q", got)
	}
}

func TestRedactDatabaseURL_Invalid(t *testing.T) {
	if got := RedactDatabaseURL("%%%"); got != "<invalid database url>" {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestRedactDatabaseURL_NoUserInfo(t *testing.T) {
	got := RedactDatabaseURL("postgres://localhost:5432/db?sslmode=disable")
	if got != "postgres://localhost:5432/db?sslmode=disable" {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestRedactDatabaseURL_UserWithoutPassword(t *testing.T) {
	got := RedactDatabaseURL("postgres://user@localhost:5432/db?sslmode=disable")
	if got != "postgres://user@localhost:5432/db?sslmode=disable" {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestRedactDatabaseURL_EmptyUsername(t *testing.T) {
	got := RedactDatabaseURL("postgres://:pass@localhost:5432/db?sslmode=disable")
	if got != "postgres://localhost:5432/db?sslmode=disable" {
		t.Fatalf("unexpected: %q", got)
	}
}

