package ui

import (
	"fmt"
	"os"
	"sync"

	"github.com/mdp/qrterminal/v3"
	"github.com/schollz/progressbar/v3"
)

// ShowRoom prints the room name, URL, and QR code.
func ShowRoom(room, url string) {
	fmt.Fprintf(os.Stderr, "\n  Room: %s\n", room)
	fmt.Fprintf(os.Stderr, "  URL:  %s\n\n", url)
	qrterminal.GenerateHalfBlock(url, qrterminal.L, os.Stderr)
	fmt.Fprintln(os.Stderr)
}

// ShowStatus prints a status line.
func ShowStatus(msg string) {
	fmt.Fprintf(os.Stderr, "  %s\n\n", msg)
}

// fileBar tracks a progress bar for one file.
type fileBar struct {
	bar  *progressbar.ProgressBar
	last int
}

var (
	barsMu sync.Mutex
	bars   = map[uint32]*fileBar{}
)

const chunkSize = 65_536

// UpdateProgress updates (or creates) the progress bar for a file.
// current and total are chunk counts; size is the total file size in bytes.
func UpdateProgress(id uint32, name string, current, total int, size int64) {
	barsMu.Lock()
	defer barsMu.Unlock()

	fb, exists := bars[id]
	if !exists {
		bar := progressbar.NewOptions64(
			size,
			progressbar.OptionSetDescription(truncate(name, 22)),
			progressbar.OptionShowBytes(true),
			progressbar.OptionSetWidth(28),
			progressbar.OptionThrottle(80),
			progressbar.OptionSetWriter(os.Stderr),
			progressbar.OptionUseANSICodes(true),
			progressbar.OptionEnableColorCodes(true),
			progressbar.OptionSetRenderBlankState(true),
			progressbar.OptionOnCompletion(func() {
				fmt.Fprintln(os.Stderr)
			}),
		)
		fb = &fileBar{bar: bar}
		bars[id] = fb
	}

	delta := current - fb.last
	if delta <= 0 {
		return
	}
	fb.last = current

	// Advance by bytes, clamping to actual file size on the last chunk.
	var advance int64
	if current >= total {
		// Jump straight to 100% by adding whatever remains.
		advance = size - int64(fb.bar.State().CurrentBytes)
		if advance < 0 {
			advance = 0
		}
	} else {
		advance = int64(delta) * chunkSize
	}
	fb.bar.Add64(advance) //nolint:errcheck
}

// FileComplete marks the bar as finished and prints the saved path.
func FileComplete(id uint32, name, path string) {
	barsMu.Lock()
	fb, ok := bars[id]
	barsMu.Unlock()
	if ok {
		fb.bar.Finish() //nolint:errcheck
	}
	fmt.Fprintf(os.Stderr, "  Saved: %s\n", path)
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		// Pad so all descriptions have the same width.
		return s + spaces(n-len(runes))
	}
	return string(runes[:n-1]) + "…"
}

func spaces(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = ' '
	}
	return string(b)
}
