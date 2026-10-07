package roomname

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"strings"
)

// Words is the number of words in a room name.
const Words = 4

var wordSet = func() map[string]bool {
	m := make(map[string]bool, len(wordlist))
	for _, w := range wordlist {
		m[w] = true
	}
	return m
}()

// Normalize turns user input — a bare code (any case, dashes or spaces) or a
// pasted https://…/r/#code link — into a canonical room name. It must match
// the web app's normalizeRoomCode.
func Normalize(input string) (string, error) {
	if i := strings.LastIndex(input, "#"); i >= 0 {
		input = input[i+1:]
	}
	words := strings.FieldsFunc(strings.ToLower(input), func(r rune) bool {
		return r == '-' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	})
	if len(words) != Words {
		return "", fmt.Errorf("room codes are %d words", Words)
	}
	for _, w := range words {
		if !wordSet[w] {
			return "", fmt.Errorf("%q isn't a room code word", w)
		}
	}
	return strings.Join(words, "-"), nil
}

// Generate returns a 4-word room name using the same EFF wordlist and
// rejection-sampling algorithm as the web app.
func Generate() string {
	words := make([]string, Words)
	for i := range words {
		words[i] = wordlist[secureRandInt(len(wordlist))]
	}
	return strings.Join(words, "-")
}

// secureRandInt returns a uniform random int in [0, max) using rejection
// sampling to avoid modulo bias, matching the web app's secureRandInt.
func secureRandInt(max int) int {
	limit := (0x1_0000_0000 / uint64(max)) * uint64(max)
	var buf [4]byte
	for {
		rand.Read(buf[:])
		v := uint64(binary.BigEndian.Uint32(buf[:]))
		if v < limit {
			return int(v % uint64(max))
		}
	}
}
