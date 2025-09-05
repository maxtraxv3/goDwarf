package main

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Test that parseMovie records the full 32-bit revision from the header.
func TestParseMovieRevision(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	src := filepath.Join(filepath.Dir(file), "clmovFiles", "test.clMov")
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	// Overwrite revision bytes with a distinctive value.
	binary.BigEndian.PutUint32(data[16:20], 0x01020304)
	tmp := filepath.Join(t.TempDir(), "rev.clMov")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	movieRevision = 0
	if _, err := parseMovie(tmp, 0); err != nil {
		t.Fatalf("parseMovie: %v", err)
	}
	if movieRevision != 0x01020304 {
		t.Fatalf("movieRevision = 0x%x, want 0x01020304", movieRevision)
	}
}
