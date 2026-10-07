package transfer

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// Shared with the web app's tests (src/lib/transfer.test.ts).
const vectorsPath = "../../../testdata/compat.json"

type compatVectors struct {
	FileKeys []struct {
		Name         string `json:"name"`
		Size         int64  `json:"size"`
		LastModified int64  `json:"lastModified"`
		Key          string `json:"key"`
	} `json:"fileKeys"`
	Have []struct {
		Name        string   `json:"name"`
		TotalChunks int      `json:"totalChunks"`
		Bitmap      string   `json:"bitmap"`
		Ranges      [][2]int `json:"ranges"`
		Missing     [][2]int `json:"missing"`
	} `json:"have"`
}

func loadVectors(t *testing.T) compatVectors {
	t.Helper()
	b, err := os.ReadFile(vectorsPath)
	if err != nil {
		t.Fatal(err)
	}
	var v compatVectors
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestFileKeyVectors(t *testing.T) {
	for _, c := range loadVectors(t).FileKeys {
		if got := fileKey(c.Name, c.Size, c.LastModified); got != c.Key {
			t.Errorf("fileKey(%q, %d, %d) = %s, want %s", c.Name, c.Size, c.LastModified, got, c.Key)
		}
	}
}

func TestHaveVectors(t *testing.T) {
	for _, c := range loadVectors(t).Have {
		t.Run(c.Name, func(t *testing.T) {
			ranges := decodeBitmap(c.Bitmap, c.TotalChunks)
			if len(ranges) != 0 || len(c.Ranges) != 0 {
				if !reflect.DeepEqual(ranges, c.Ranges) {
					t.Errorf("decodeBitmap = %v, want %v", ranges, c.Ranges)
				}
			}
			missing := invertRanges(c.Ranges, c.TotalChunks)
			if len(missing) != 0 || len(c.Missing) != 0 {
				if !reflect.DeepEqual(missing, c.Missing) {
					t.Errorf("invertRanges = %v, want %v", missing, c.Missing)
				}
			}
		})
	}
}

func TestNonCollidingPath(t *testing.T) {
	dir := t.TempDir()
	if got := nonCollidingPath(dir, "a.txt"); got != filepath.Join(dir, "a.txt") {
		t.Errorf("free name: got %s", got)
	}
	os.WriteFile(filepath.Join(dir, "a.txt"), nil, 0o644)     //nolint:errcheck
	os.WriteFile(filepath.Join(dir, "a (1).txt"), nil, 0o644) //nolint:errcheck
	if got := nonCollidingPath(dir, "a.txt"); got != filepath.Join(dir, "a (2).txt") {
		t.Errorf("taken name: got %s", got)
	}
}

// link delivers one manager's outgoing messages to another, the way the
// data channels would: control messages round-trip through JSON and chunk
// buffers are copied (the sender reuses its packet buffer).
type link struct{ to *Manager }

func (l link) SendControl(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var msg map[string]any
	if err := json.Unmarshal(b, &msg); err != nil {
		return err
	}
	l.to.HandleControl(msg)
	return nil
}

func (l link) SendChunk(b []byte) error {
	l.to.HandleData(bytes.Clone(b))
	return nil
}

// transferFiles sends files from a fresh sender to a fresh receiver and
// returns the receiver's output directory once everything has arrived.
func transferFiles(t *testing.T, files map[string][]byte) string {
	t.Helper()
	srcDir, outDir := t.TempDir(), t.TempDir()
	var paths []string
	for name, data := range files {
		p := filepath.Join(srcDir, name)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}

	sent, received := make(chan struct{}, 1), make(chan struct{}, 1)
	sender := NewManager("", Callbacks{OnAllSent: func() { sent <- struct{}{} }})
	receiver := NewManager(outDir, Callbacks{OnAllReceived: func() { received <- struct{}{} }})
	sender.SetSender(link{receiver})
	receiver.SetSender(link{sender})

	if err := sender.SendFiles(paths); err != nil {
		t.Fatal(err)
	}
	for _, ch := range []chan struct{}{sent, received} {
		select {
		case <-ch:
		case <-time.After(5 * time.Second):
			t.Fatal("transfer timed out")
		}
	}
	return outDir
}

func TestTransfer(t *testing.T) {
	big := make([]byte, 3*ChunkSize+123)
	for i := range big {
		big[i] = byte(i * 31)
	}
	files := map[string][]byte{
		"big.bin":   big,
		"exact.bin": bytes.Repeat([]byte{7}, ChunkSize),
		"small.txt": []byte("hello"),
		"empty":     {},
	}
	outDir := transferFiles(t, files)
	for name, want := range files {
		got, err := os.ReadFile(filepath.Join(outDir, name))
		if err != nil {
			t.Errorf("%s: %v", name, err)
		} else if !bytes.Equal(got, want) {
			t.Errorf("%s: received %d bytes that differ from the %d sent", name, len(got), len(want))
		}
	}
	if tmp, _ := filepath.Glob(filepath.Join(outDir, ".filedrop-*.part")); len(tmp) > 0 {
		t.Errorf("temp files left behind: %v", tmp)
	}
}

func TestCleanupRemovesPartialFiles(t *testing.T) {
	outDir := t.TempDir()
	m := NewManager(outDir, Callbacks{})
	m.SetSender(recorder{new([]map[string]any)})
	m.HandleControl(map[string]any{"type": "manifest", "files": []any{
		map[string]any{"id": 1.0, "name": "a.bin", "size": float64(2 * ChunkSize), "totalChunks": 2.0},
		map[string]any{"id": 2.0, "name": "b.bin", "size": float64(ChunkSize), "totalChunks": 1.0},
	}})
	chunk := make([]byte, 8+ChunkSize)
	chunk[3] = 1 // file 1, chunk 0 of 2
	m.HandleData(chunk)

	m.Cleanup()
	if entries, _ := os.ReadDir(outDir); len(entries) > 0 {
		t.Errorf("files left behind: %v", entries)
	}
	// Late chunks and manifests after cleanup must not recreate anything
	chunk[7] = 1
	m.HandleData(chunk)
	m.HandleControl(map[string]any{"type": "manifest", "files": []any{
		map[string]any{"id": 3.0, "name": "c.bin", "size": 1.0, "totalChunks": 1.0},
	}})
	if entries, _ := os.ReadDir(outDir); len(entries) > 0 {
		t.Errorf("files created after cleanup: %v", entries)
	}
}

func TestHaveAllMarksSent(t *testing.T) {
	src := filepath.Join(t.TempDir(), "f")
	os.WriteFile(src, make([]byte, 2*ChunkSize), 0o644) //nolint:errcheck

	var msgs []map[string]any
	sent := make(chan struct{}, 1)
	m := NewManager("", Callbacks{OnAllSent: func() { sent <- struct{}{} }})
	m.SetSender(recorder{&msgs})
	if err := m.SendFiles([]string{src}); err != nil {
		t.Fatal(err)
	}
	// The receiver already has every chunk (e.g. from an earlier session)
	m.HandleControl(map[string]any{"type": "have", "fileId": float64(1), "ranges": []any{[]any{0.0, 1.0}}})
	select {
	case <-sent:
	case <-time.After(time.Second):
		t.Fatal("file not marked as sent")
	}
}

type recorder struct{ msgs *[]map[string]any }

func (r recorder) SendControl(v any) error {
	*r.msgs = append(*r.msgs, v.(map[string]any))
	return nil
}
func (r recorder) SendChunk([]byte) error { panic("no chunks expected") }
