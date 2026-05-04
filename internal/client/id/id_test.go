package id_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/HMasataka/axion/internal/client/id"
)

func TestLoadOrCreate_GeneratesNewID(t *testing.T) {
	dir := t.TempDir()
	idFile := filepath.Join(dir, "sub", "client.id")

	// Given: idFile does not exist
	got, err := id.LoadOrCreate(idFile)
	if err != nil {
		t.Fatalf("LoadOrCreate: unexpected error: %v", err)
	}

	// When: parsed as UUID
	if _, err := uuid.Parse(got); err != nil {
		t.Fatalf("generated value is not a valid UUID: %v", err)
	}

	// Then: subsequent load returns same ID
	got2, err := id.LoadOrCreate(idFile)
	if err != nil {
		t.Fatalf("LoadOrCreate second call: unexpected error: %v", err)
	}
	if got != got2 {
		t.Fatalf("expected same ID on second load, got %q != %q", got, got2)
	}
}

func TestLoadOrCreate_ReadsExistingID(t *testing.T) {
	dir := t.TempDir()
	idFile := filepath.Join(dir, "client.id")

	existing := uuid.New().String()
	if err := os.WriteFile(idFile, []byte(existing+"\n"), 0600); err != nil { //nolint:forbidigo
		t.Fatalf("write fixture: %v", err)
	}

	// Given: existing valid UUID file
	got, err := id.LoadOrCreate(idFile)
	if err != nil {
		t.Fatalf("LoadOrCreate: unexpected error: %v", err)
	}

	// Then: returns existing ID without modification
	if got != existing {
		t.Fatalf("expected %q, got %q", existing, got)
	}
}

func TestLoadOrCreate_RejectsInvalidContent(t *testing.T) {
	dir := t.TempDir()
	idFile := filepath.Join(dir, "client.id")

	if err := os.WriteFile(idFile, []byte("not-a-uuid"), 0600); err != nil { //nolint:forbidigo
		t.Fatalf("write fixture: %v", err)
	}

	// Given: file with invalid UUID content
	_, err := id.LoadOrCreate(idFile)
	if err == nil {
		t.Fatal("expected error for invalid UUID content, got nil")
	}
}
