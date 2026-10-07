package transfer

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const ChunkSize = 65_536

// Sender is implemented by peer.Peer.
type Sender interface {
	SendControl(v any) error
	SendChunk(b []byte) error
}

type Callbacks struct {
	OnSendProgress    func(name string, sent, total int, size int64)
	OnReceiveProgress func(name string, received, total int, size int64)
	OnFileComplete    func(name, finalPath string)
	OnAllSent         func()
	OnAllReceived     func()
}

type fileInfo struct {
	ID          uint32 `json:"id"`
	Key         string `json:"key,omitempty"`
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	TotalChunks int    `json:"totalChunks"`
}

type localFile struct {
	info       fileInfo
	path       string
	sentChunks int
	done       bool
}

type remoteFile struct {
	info          fileInfo
	received      []byte
	receivedCount int
	tmpPath       string
	f             *os.File
	writeMu       sync.Mutex
	finalizing    bool // finalizeFile or Cleanup has claimed it
	done          bool // renamed into outDir
}

type Manager struct {
	sender Sender
	outDir string
	cb     Callbacks

	mu     sync.Mutex
	local  map[uint32]*localFile
	remote map[uint32]*remoteFile
	nextID uint32
	closed bool // Cleanup has run; no new temp files

	// renames counts finalizeFile calls between claiming a file and renaming
	// it, so Cleanup can wait for them instead of leaving a temp file behind.
	renames sync.WaitGroup

	// sendQ serialises chunk sends so only one file is in-flight at a time,
	// matching the activeSend promise chain in the web app.
	sendQ chan func()
}

func NewManager(outDir string, cb Callbacks) *Manager {
	m := &Manager{
		outDir: outDir,
		cb:     cb,
		local:  make(map[uint32]*localFile),
		remote: make(map[uint32]*remoteFile),
		nextID: 1,
		sendQ:  make(chan func(), 32),
	}
	go func() {
		for task := range m.sendQ {
			task()
		}
	}()
	return m
}

func (m *Manager) SetSender(s Sender) {
	m.mu.Lock()
	m.sender = s
	m.mu.Unlock()
}

// SendFiles announces all files to the remote peer. Call after connected.
func (m *Manager) SendFiles(paths []string) error {
	var infos []fileInfo
	m.mu.Lock()
	for _, p := range paths {
		stat, err := os.Stat(p)
		if err != nil {
			m.mu.Unlock()
			return fmt.Errorf("%s: %w", p, err)
		}
		// An empty file has zero chunks, as in the web app: the receiver
		// finishes it as soon as it's offered.
		totalChunks := int((stat.Size() + ChunkSize - 1) / ChunkSize)
		id := m.nextID
		m.nextID++
		fi := fileInfo{
			ID:          id,
			Key:         fileKey(filepath.Base(p), stat.Size(), stat.ModTime().UnixMilli()),
			Name:        filepath.Base(p),
			Size:        stat.Size(),
			TotalChunks: totalChunks,
		}
		m.local[id] = &localFile{info: fi, path: p}
		infos = append(infos, fi)
	}
	s := m.sender
	m.mu.Unlock()

	return s.SendControl(map[string]any{"type": "manifest", "files": infos})
}

// fileKey fingerprints a file for resume across connections. Must match fileKey() in the web app.
func fileKey(name string, size int64, modMillis int64) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\n%d\n%d", name, size, modMillis)))
	return hex.EncodeToString(sum[:16])
}

// HandleControl dispatches an incoming control-channel message.
func (m *Manager) HandleControl(msg map[string]any) {
	switch msg["type"] {
	case "manifest":
		m.handleManifest(msg)
	case "have":
		m.handleHave(msg)
	}
}

func (m *Manager) handleManifest(msg map[string]any) {
	raw, _ := msg["files"].([]any)
	b, _ := json.Marshal(raw)
	var files []fileInfo
	if json.Unmarshal(b, &files) != nil {
		return
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	s := m.sender
	for _, fi := range files {
		if _, exists := m.remote[fi.ID]; exists {
			continue
		}
		// Unique name, so concurrent receives into the same directory can't collide
		f, err := os.CreateTemp(m.outDir, ".filedrop-*.part")
		if err != nil {
			continue
		}
		tmpPath := f.Name()
		if fi.Size > 0 {
			f.Truncate(fi.Size) //nolint:errcheck
		}
		bitmapLen := (fi.TotalChunks + 7) / 8
		m.remote[fi.ID] = &remoteFile{
			info:     fi,
			received: make([]byte, bitmapLen),
			tmpPath:  tmpPath,
			f:        f,
		}
	}
	m.mu.Unlock()

	// Send "have" for each file with empty ranges (we have nothing yet)
	for _, fi := range files {
		s.SendControl(map[string]any{ //nolint:errcheck
			"type":   "have",
			"fileId": fi.ID,
			"ranges": []any{},
		})
		if m.cb.OnReceiveProgress != nil {
			m.cb.OnReceiveProgress(fi.Name, 0, fi.TotalChunks, fi.Size)
		}
		if fi.TotalChunks == 0 {
			go m.finalizeFile(fi.ID)
		}
	}
}

func (m *Manager) handleHave(msg map[string]any) {
	fileID := toUint32(msg["fileId"])
	m.mu.Lock()
	lf := m.local[fileID]
	m.mu.Unlock()
	if lf == nil {
		return
	}

	var haveRanges [][2]int
	if bm, ok := msg["bitmap"].(string); ok {
		haveRanges = decodeBitmap(bm, lf.info.TotalChunks)
	} else if raw, ok := msg["ranges"].([]any); ok {
		for _, r := range raw {
			pair, ok := r.([]any)
			if !ok || len(pair) != 2 {
				continue
			}
			s := int(toFloat(pair[0]))
			e := int(toFloat(pair[1]))
			haveRanges = append(haveRanges, [2]int{s, e})
		}
	}

	missing := invertRanges(haveRanges, lf.info.TotalChunks)
	if len(missing) == 0 {
		// Receiver already has the whole file (e.g. from an earlier session)
		m.mu.Lock()
		lf.sentChunks = lf.info.TotalChunks
		lf.done = true
		allDone := m.allLocalDone()
		m.mu.Unlock()
		if m.cb.OnSendProgress != nil {
			m.cb.OnSendProgress(lf.info.Name, lf.info.TotalChunks, lf.info.TotalChunks, lf.info.Size)
		}
		if allDone && m.cb.OnAllSent != nil {
			m.cb.OnAllSent()
		}
		return
	}

	id := fileID
	ranges := missing
	m.sendQ <- func() { m.sendChunks(id, ranges) }
}

func (m *Manager) sendChunks(fileID uint32, ranges [][2]int) {
	m.mu.Lock()
	lf := m.local[fileID]
	s := m.sender
	m.mu.Unlock()
	if lf == nil || s == nil {
		return
	}

	f, err := os.Open(lf.path)
	if err != nil {
		return
	}
	defer f.Close()

	buf := make([]byte, ChunkSize)
	packet := make([]byte, 8+ChunkSize)

	for _, r := range ranges {
		for i := r[0]; i <= r[1]; i++ {
			n, err := f.ReadAt(buf, int64(i)*ChunkSize)
			if err != nil && err != io.EOF {
				return
			}
			if n == 0 {
				continue
			}
			binary.BigEndian.PutUint32(packet[0:4], fileID)
			binary.BigEndian.PutUint32(packet[4:8], uint32(i))
			copy(packet[8:], buf[:n])

			if err := s.SendChunk(packet[:8+n]); err != nil {
				return
			}

			m.mu.Lock()
			lf.sentChunks++
			sent := lf.sentChunks
			total := lf.info.TotalChunks
			lf.done = sent >= total
			allDone := m.allLocalDone()
			m.mu.Unlock()

			if m.cb.OnSendProgress != nil {
				m.cb.OnSendProgress(lf.info.Name, sent, total, lf.info.Size)
			}
			if allDone && m.cb.OnAllSent != nil {
				m.cb.OnAllSent()
			}
		}
	}
}

// HandleData processes an incoming binary data-channel message.
func (m *Manager) HandleData(buf []byte) {
	if len(buf) < 8 {
		return
	}
	fileID := binary.BigEndian.Uint32(buf[0:4])
	chunkIdx := int(binary.BigEndian.Uint32(buf[4:8]))
	data := buf[8:]

	m.mu.Lock()
	entry := m.remote[fileID]
	m.mu.Unlock()
	if entry == nil {
		return
	}

	byteIdx := chunkIdx >> 3
	bitMask := byte(1 << (chunkIdx & 7))

	entry.writeMu.Lock()
	if entry.received[byteIdx]&bitMask != 0 {
		entry.writeMu.Unlock()
		return
	}
	entry.received[byteIdx] |= bitMask
	entry.receivedCount++
	received := entry.receivedCount
	total := entry.info.TotalChunks

	// WriteAt is safe to call with the writeMu held — serialised per file.
	entry.f.WriteAt(data, int64(chunkIdx)*ChunkSize) //nolint:errcheck
	entry.writeMu.Unlock()

	if m.cb.OnReceiveProgress != nil {
		m.cb.OnReceiveProgress(entry.info.Name, received, total, entry.info.Size)
	}
	if received >= total {
		go m.finalizeFile(fileID)
	}
}

func (m *Manager) finalizeFile(fileID uint32) {
	m.mu.Lock()
	entry := m.remote[fileID]
	if entry == nil || entry.finalizing {
		m.mu.Unlock()
		return
	}
	entry.finalizing = true
	m.renames.Add(1)
	m.mu.Unlock()

	entry.writeMu.Lock()
	if entry.f != nil {
		entry.f.Close()
		entry.f = nil
	}
	entry.writeMu.Unlock()

	finalPath := nonCollidingPath(m.outDir, entry.info.Name)
	err := os.Rename(entry.tmpPath, finalPath)
	m.renames.Done()
	if err != nil {
		os.Remove(entry.tmpPath) //nolint:errcheck
		return
	}

	if m.cb.OnFileComplete != nil {
		m.cb.OnFileComplete(entry.info.Name, finalPath)
	}

	// Mark done and check in one step, so only the last file to finish
	// reports completion, and only once every file has been renamed.
	m.mu.Lock()
	entry.done = true
	allDone := m.allRemoteDone()
	m.mu.Unlock()

	if allDone && m.cb.OnAllReceived != nil {
		m.cb.OnAllReceived()
	}
}

// Cleanup deletes the temp files of incomplete downloads and stops accepting
// new ones. Call it once the transfer is over, whether it finished or not;
// files that are already complete are still renamed into place.
func (m *Manager) Cleanup() {
	m.mu.Lock()
	m.closed = true
	var unfinished []*remoteFile
	for _, r := range m.remote {
		if !r.finalizing {
			r.finalizing = true
			unfinished = append(unfinished, r)
		}
	}
	m.mu.Unlock()

	for _, r := range unfinished {
		r.writeMu.Lock()
		if r.f != nil {
			r.f.Close()
			r.f = nil
		}
		r.writeMu.Unlock()
		os.Remove(r.tmpPath) //nolint:errcheck
	}
	m.renames.Wait()
}

// ── Helpers ────────────────────────────────────────────────────────────────────

func (m *Manager) allLocalDone() bool {
	for _, l := range m.local {
		if !l.done {
			return false
		}
	}
	return len(m.local) > 0
}

func (m *Manager) allRemoteDone() bool {
	for _, r := range m.remote {
		if !r.done {
			return false
		}
	}
	return true
}

func nonCollidingPath(dir, name string) string {
	p := filepath.Join(dir, name)
	if _, err := os.Stat(p); os.IsNotExist(err) {
		return p
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 1; ; i++ {
		p = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", base, i, ext))
		if _, err := os.Stat(p); os.IsNotExist(err) {
			return p
		}
	}
}

func invertRanges(have [][2]int, total int) [][2]int {
	var missing [][2]int
	pos := 0
	for _, r := range have {
		if pos < r[0] {
			missing = append(missing, [2]int{pos, r[0] - 1})
		}
		pos = r[1] + 1
	}
	if pos < total {
		missing = append(missing, [2]int{pos, total - 1})
	}
	return missing
}

func decodeBitmap(b64 string, totalChunks int) [][2]int {
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil
	}
	need := (totalChunks + 7) / 8
	if len(data) < need {
		data = append(data, make([]byte, need-len(data))...)
	}
	return bitmapToRanges(data[:need], totalChunks)
}

func bitmapToRanges(bm []byte, total int) [][2]int {
	var ranges [][2]int
	start := -1
	for i := 0; i <= total; i++ {
		has := i < total && ((bm[i>>3]>>(i&7))&1) == 1
		if has && start == -1 {
			start = i
		} else if !has && start != -1 {
			ranges = append(ranges, [2]int{start, i - 1})
			start = -1
		}
	}
	return ranges
}

func toUint32(v any) uint32 {
	switch n := v.(type) {
	case float64:
		return uint32(n)
	case json.Number:
		i, _ := n.Int64()
		return uint32(i)
	}
	return 0
}

func toFloat(v any) float64 {
	f, _ := v.(float64)
	return f
}
