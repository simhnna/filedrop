package roomname

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// Shared with the web app's tests (src/lib/roomName.test.ts).
const vectorsPath = "../../../testdata/compat.json"

func TestNormalizeVectors(t *testing.T) {
	b, err := os.ReadFile(vectorsPath)
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		RoomCodes []struct {
			Input  string  `json:"input"`
			RoomID *string `json:"roomId"`
		} `json:"roomCodes"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	for _, c := range v.RoomCodes {
		got, err := Normalize(c.Input)
		switch {
		case c.RoomID == nil && err == nil:
			t.Errorf("Normalize(%q) = %q, want error", c.Input, got)
		case c.RoomID != nil && err != nil:
			t.Errorf("Normalize(%q) failed: %v", c.Input, err)
		case c.RoomID != nil && got != *c.RoomID:
			t.Errorf("Normalize(%q) = %q, want %q", c.Input, got, *c.RoomID)
		}
	}
}

func TestWordlist(t *testing.T) {
	if len(wordSet) != len(wordlist) {
		t.Errorf("wordlist has %d duplicates", len(wordlist)-len(wordSet))
	}
	for _, w := range wordlist {
		if w == "" || w != strings.ToLower(w) || strings.ContainsAny(w, "- \t") {
			t.Errorf("word %q can't round-trip through Normalize", w)
		}
	}
}

func TestGenerate(t *testing.T) {
	for range 100 {
		name := Generate()
		got, err := Normalize(name)
		if err != nil || got != name {
			t.Fatalf("Generate() = %q, which doesn't normalize to itself (%q, %v)", name, got, err)
		}
	}
}

func TestSecureRandIntRange(t *testing.T) {
	seen := make([]bool, 7)
	for range 1000 {
		n := secureRandInt(len(seen))
		if n < 0 || n >= len(seen) {
			t.Fatalf("secureRandInt(%d) = %d", len(seen), n)
		}
		seen[n] = true
	}
	for i, ok := range seen {
		if !ok {
			t.Errorf("value %d never produced in 1000 draws", i)
		}
	}
}
