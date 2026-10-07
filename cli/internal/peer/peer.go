package peer

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"filedrop-cli/internal/pake"
	"filedrop-cli/internal/signaling"

	"github.com/pion/webrtc/v4"
)

const (
	bufferHigh = 1_048_576
	bufferLow  = 262_144

	// Give up on a connection whose PAKE hasn't completed by then
	authTimeout = 15 * time.Second
)

var (
	ErrClosed     = errors.New("peer closed")
	ErrAuthFailed = errors.New("could not verify the other side (wrong room code, or the connection was intercepted)")
)

type Events struct {
	OnConnected    func()
	OnDisconnected func()
	OnAuthFailed   func()
	OnControl      func(msg map[string]any)
	OnData         func(buf []byte)
}

type Peer struct {
	peerId string
	role   string
	secret string
	sig    *signaling.Signaling
	events Events

	pc            *webrtc.PeerConnection
	controlCh     *webrtc.DataChannel
	dataCh        *webrtc.DataChannel
	remotePeerId  string
	offerSent     bool
	authenticated bool
	authFailed    bool
	remoteDescSet bool
	pake          *pake.Pake

	pendingCandidates []webrtc.ICECandidateInit

	bufferLowCh chan struct{}
	done        chan struct{}
	closeOnce   sync.Once
	mu          sync.Mutex
}

var iceServers = []webrtc.ICEServer{
	{URLs: []string{"stun:stun.cloudflare.com:3478"}},
}

// New starts connecting. secret is the room's PAKE secret; it is never sent anywhere.
func New(sig *signaling.Signaling, role, secret string, events Events) *Peer {
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{ICEServers: iceServers})
	if err != nil {
		panic(err)
	}

	p := &Peer{
		peerId:      newUUID(),
		role:        role,
		secret:      secret,
		sig:         sig,
		events:      events,
		pc:          pc,
		bufferLowCh: make(chan struct{}, 1),
		done:        make(chan struct{}),
	}

	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		p.mu.Lock()
		remote := p.remotePeerId
		p.mu.Unlock()
		if remote == "" {
			return
		}
		init := c.ToJSON()
		sig.Publish(map[string]any{
			"type": "ice",
			"from": p.peerId,
			"to":   remote,
			"candidate": map[string]any{
				"candidate":     init.Candidate,
				"sdpMid":        derefStr(init.SDPMid),
				"sdpMLineIndex": derefU16(init.SDPMLineIndex),
			},
		})
	})

	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		switch state {
		case webrtc.PeerConnectionStateDisconnected,
			webrtc.PeerConnectionStateFailed,
			webrtc.PeerConnectionStateClosed:
			p.events.OnDisconnected()
		}
	})

	if role == "host" {
		p.setupDataChannels()
	} else {
		pc.OnDataChannel(func(d *webrtc.DataChannel) {
			switch d.Label() {
			case "control":
				p.bindControl(d)
			case "data":
				p.bindData(d)
			}
		})
	}

	sig.OnMessage(p.handleSignaling)
	p.startAnnouncing()
	return p
}

// ── Data channels ──────────────────────────────────────────────────────────────

func (p *Peer) setupDataChannels() {
	ordered := true
	init := &webrtc.DataChannelInit{Ordered: &ordered}
	ctrl, _ := p.pc.CreateDataChannel("control", init)
	p.bindControl(ctrl)
	data, _ := p.pc.CreateDataChannel("data", init)
	p.bindData(data)
}

func (p *Peer) bindControl(ch *webrtc.DataChannel) {
	p.mu.Lock()
	p.controlCh = ch
	p.mu.Unlock()

	ch.OnOpen(func() { p.onControlOpen() })
	ch.OnMessage(func(msg webrtc.DataChannelMessage) {
		if !msg.IsString {
			return
		}
		var m map[string]any
		if json.Unmarshal(msg.Data, &m) != nil {
			return
		}
		p.mu.Lock()
		auth := p.authenticated
		p.mu.Unlock()
		if !auth {
			p.handleAuthMsg(m)
		} else {
			p.events.OnControl(m)
		}
	})
}

func (p *Peer) bindData(ch *webrtc.DataChannel) {
	p.mu.Lock()
	p.dataCh = ch
	p.mu.Unlock()

	ch.SetBufferedAmountLowThreshold(bufferLow)
	ch.OnBufferedAmountLow(func() {
		select {
		case p.bufferLowCh <- struct{}{}:
		default:
		}
	})
	ch.OnMessage(func(msg webrtc.DataChannelMessage) {
		p.mu.Lock()
		auth := p.authenticated
		p.mu.Unlock()
		if !msg.IsString && auth {
			p.events.OnData(msg.Data)
		}
	})
}

// ── Auth ───────────────────────────────────────────────────────────────────────
//
// Both sides run the PAKE (see internal/pake) as soon as the control channel opens:
//   → {type: "pake", share}          our public share
//   → {type: "pake-confirm", mac}    sent once we've seen the other share
// The connection counts as established only after the other side's MAC checks
// out, which proves it knows the room secret and sees the same DTLS fingerprints.

func (p *Peer) onControlOpen() {
	pk := p.ensurePake()
	if pk == nil {
		return
	}
	p.sendControlInternal(map[string]any{"type": "pake", "share": base64.StdEncoding.EncodeToString(pk.Share())})
	time.AfterFunc(authTimeout, p.failAuth)
}

// ensurePake creates the PAKE lazily: the other side's share may arrive before our OnOpen.
func (p *Peer) ensurePake() *pake.Pake {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pake != nil {
		return p.pake
	}
	local, remote := p.pc.LocalDescription(), p.pc.RemoteDescription()
	if local == nil || remote == nil {
		go p.failAuth()
		return nil
	}
	hostFp, peerFp := pake.SDPFingerprints(local.SDP), pake.SDPFingerprints(remote.SDP)
	if p.role == "peer" {
		hostFp, peerFp = peerFp, hostFp
	}
	p.pake = pake.New(p.secret, p.role, hostFp, peerFp)
	return p.pake
}

func (p *Peer) handleAuthMsg(m map[string]any) {
	pk := p.ensurePake()
	if pk == nil {
		return
	}
	switch m["type"] {
	case "pake":
		share, err := decodeB64(m["share"])
		if err != nil {
			p.failAuth()
			return
		}
		mac, err := pk.ReceiveShare(share)
		if err != nil {
			p.failAuth()
			return
		}
		p.sendControlInternal(map[string]any{"type": "pake-confirm", "mac": base64.StdEncoding.EncodeToString(mac)})
	case "pake-confirm":
		mac, err := decodeB64(m["mac"])
		if err != nil || !pk.VerifyConfirm(mac) {
			p.failAuth()
			return
		}
		p.mu.Lock()
		if p.authFailed {
			p.mu.Unlock()
			return
		}
		p.authenticated = true
		p.mu.Unlock()
		p.events.OnConnected()
	}
}

func (p *Peer) failAuth() {
	p.mu.Lock()
	if p.authFailed || p.authenticated {
		p.mu.Unlock()
		return
	}
	p.authFailed = true
	p.mu.Unlock()
	if p.events.OnAuthFailed != nil {
		p.events.OnAuthFailed()
	}
	p.Close()
}

func decodeB64(v any) ([]byte, error) {
	s, ok := v.(string)
	if !ok {
		return nil, errors.New("not a string")
	}
	return base64.StdEncoding.DecodeString(s)
}

// ── Signaling ──────────────────────────────────────────────────────────────────

func (p *Peer) handleSignaling(msg map[string]any) {
	if msg["from"] == p.peerId {
		return
	}
	switch msg["type"] {
	case "announce":
		theirRole, _ := msg["role"].(string)
		theirId, _ := msg["peerId"].(string)
		p.mu.Lock()
		if p.role == "host" && theirRole == "peer" && !p.offerSent {
			p.offerSent = true
			p.remotePeerId = theirId
			p.mu.Unlock()
			go p.createOffer()
		} else if p.role == "peer" && theirRole == "host" {
			p.remotePeerId = theirId
			p.mu.Unlock()
		} else {
			p.mu.Unlock()
		}

	case "offer":
		if msg["to"] != p.peerId {
			return
		}
		sdp, _ := msg["sdp"].(string)
		from, _ := msg["from"].(string)
		p.mu.Lock()
		p.remotePeerId = from
		p.mu.Unlock()
		go p.handleOffer(sdp)

	case "answer":
		if msg["to"] != p.peerId {
			return
		}
		sdp, _ := msg["sdp"].(string)
		go p.handleAnswer(sdp)

	case "ice":
		if msg["to"] != p.peerId {
			return
		}
		c, ok := msg["candidate"].(map[string]any)
		if !ok {
			return
		}
		p.handleCandidate(parseCandidate(c))
	}
}

func (p *Peer) createOffer() {
	offer, err := p.pc.CreateOffer(nil)
	if err != nil {
		return
	}
	if err := p.pc.SetLocalDescription(offer); err != nil {
		return
	}
	p.mu.Lock()
	remote := p.remotePeerId
	p.mu.Unlock()
	p.sig.Publish(map[string]any{
		"type": "offer",
		"from": p.peerId,
		"to":   remote,
		"sdp":  offer.SDP,
	})
}

func (p *Peer) handleOffer(sdp string) {
	if err := p.pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer, SDP: sdp,
	}); err != nil {
		return
	}
	p.mu.Lock()
	p.remoteDescSet = true
	pending := p.pendingCandidates
	p.pendingCandidates = nil
	remote := p.remotePeerId
	p.mu.Unlock()
	for _, c := range pending {
		p.pc.AddICECandidate(c) //nolint:errcheck
	}
	answer, err := p.pc.CreateAnswer(nil)
	if err != nil {
		return
	}
	if err := p.pc.SetLocalDescription(answer); err != nil {
		return
	}
	p.sig.Publish(map[string]any{
		"type": "answer",
		"from": p.peerId,
		"to":   remote,
		"sdp":  answer.SDP,
	})
}

func (p *Peer) handleAnswer(sdp string) {
	if err := p.pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer, SDP: sdp,
	}); err != nil {
		return
	}
	p.mu.Lock()
	p.remoteDescSet = true
	pending := p.pendingCandidates
	p.pendingCandidates = nil
	p.mu.Unlock()
	for _, c := range pending {
		p.pc.AddICECandidate(c) //nolint:errcheck
	}
}

func (p *Peer) handleCandidate(c webrtc.ICECandidateInit) {
	p.mu.Lock()
	if !p.remoteDescSet {
		p.pendingCandidates = append(p.pendingCandidates, c)
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()
	p.pc.AddICECandidate(c) //nolint:errcheck
}

// ── Announcing ─────────────────────────────────────────────────────────────────

func (p *Peer) startAnnouncing() {
	p.announce()
	go func() {
		ticker := time.NewTicker(4 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				p.mu.Lock()
				auth := p.authenticated
				p.mu.Unlock()
				if auth {
					return
				}
				p.announce()
			case <-p.done:
				return
			}
		}
	}()
}

func (p *Peer) announce() {
	p.sig.Publish(map[string]any{
		"type":   "announce",
		"role":   p.role,
		"peerId": p.peerId,
	})
}

// ── Public send API ────────────────────────────────────────────────────────────

func (p *Peer) SendControl(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	p.mu.Lock()
	ch := p.controlCh
	p.mu.Unlock()
	if ch == nil {
		return ErrClosed
	}
	return ch.SendText(string(b))
}

// SendChunk blocks when the data channel buffer is full, then sends buf.
func (p *Peer) SendChunk(buf []byte) error {
	p.mu.Lock()
	ch := p.dataCh
	p.mu.Unlock()
	if ch == nil {
		return ErrClosed
	}
	for ch.BufferedAmount() >= bufferHigh {
		select {
		case <-p.bufferLowCh:
		case <-p.done:
			return ErrClosed
		}
	}
	return ch.Send(buf)
}

func (p *Peer) Close() {
	p.closeOnce.Do(func() {
		close(p.done)
		p.pc.Close() //nolint:errcheck
	})
}

// ── Helpers ────────────────────────────────────────────────────────────────────

func (p *Peer) sendControlInternal(v any) {
	b, _ := json.Marshal(v)
	p.mu.Lock()
	ch := p.controlCh
	p.mu.Unlock()
	if ch != nil {
		ch.SendText(string(b)) //nolint:errcheck
	}
}

func parseCandidate(m map[string]any) webrtc.ICECandidateInit {
	c := webrtc.ICECandidateInit{}
	if s, ok := m["candidate"].(string); ok {
		c.Candidate = s
	}
	if s, ok := m["sdpMid"].(string); ok {
		c.SDPMid = &s
	}
	if f, ok := m["sdpMLineIndex"].(float64); ok {
		u := uint16(f)
		c.SDPMLineIndex = &u
	}
	return c
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func derefU16(u *uint16) uint16 {
	if u == nil {
		return 0
	}
	return *u
}

func newUUID() string {
	var b [16]byte
	rand.Read(b[:]) //nolint:errcheck
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
