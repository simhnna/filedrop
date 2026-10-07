// Package pake implements the CPace-style PAKE over ristretto255 that
// authenticates filedrop connections. It must stay byte-for-byte compatible
// with src/lib/pake.ts in the web app.
//
// Both sides know the room secret (the words after the signaling topic). Each
// derives a generator from the secret and both DTLS fingerprints, exchanges
// one point, and the two sides prove to each other that they derived the same
// key. A signaling server that swapped the SDP fingerprints (MITM) ends up with
// different generators on each half and can't complete the exchange without
// guessing the secret — and it gets one guess per connection attempt.
package pake

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/binary"
	"errors"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/gtank/ristretto255"
)

// lv is a length-prefixed concatenation (4-byte big-endian length before each part).
func lv(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = binary.BigEndian.AppendUint32(out, uint32(len(p)))
		out = append(out, p...)
	}
	return out
}

var fingerprintRe = regexp.MustCompile(`(?m)^a=fingerprint:(\S+) (\S+)\s*$`)

// SDPFingerprints returns the canonical form of the DTLS fingerprints in an SDP:
// every a=fingerprint line as "<alg lowercase> <hex uppercase>", deduplicated,
// sorted, comma-joined.
func SDPFingerprints(sdp string) string {
	seen := map[string]bool{}
	var fps []string
	for _, m := range fingerprintRe.FindAllStringSubmatch(sdp, -1) {
		fp := strings.ToLower(m[1]) + " " + strings.ToUpper(m[2])
		if !seen[fp] {
			seen[fp] = true
			fps = append(fps, fp)
		}
	}
	sort.Strings(fps)
	return strings.Join(fps, ",")
}

type Pake struct {
	role   string // "host" or "peer"
	hostFp string
	peerFp string
	scalar *ristretto255.Scalar
	share  []byte
	isk    []byte
}

func New(secret, role, hostFp, peerFp string) *Pake {
	return newWithRand(secret, role, hostFp, peerFp, rand.Reader)
}

// newWithRand is New with an explicit randomness source, so tests can derive
// deterministic shares that the web app's tests check against.
func newWithRand(secret, role, hostFp, peerFp string, rnd io.Reader) *Pake {
	h := sha512.Sum512(lv([]byte("filedrop-cpace-v1"), []byte(secret), []byte(hostFp), []byte(peerFp)))
	g, err := ristretto255.NewIdentityElement().SetUniformBytes(h[:])
	if err != nil {
		panic(err)
	}
	var buf [64]byte
	s := ristretto255.NewScalar()
	for s.Equal(ristretto255.NewScalar()) == 1 {
		io.ReadFull(rnd, buf[:])  //nolint:errcheck
		s.SetUniformBytes(buf[:]) //nolint:errcheck // buf is always 64 bytes
	}
	return &Pake{
		role:   role,
		hostFp: hostFp,
		peerFp: peerFp,
		scalar: s,
		share:  ristretto255.NewIdentityElement().ScalarMult(s, g).Bytes(),
	}
}

// Share is our public share, to send as {type: "pake", share}.
func (p *Pake) Share() []byte { return p.share }

// ReceiveShare processes the other side's share and returns our confirmation
// MAC. It fails on a malformed or degenerate share, or if called twice: every
// share we answer gives the other side one guess at the secret.
func (p *Pake) ReceiveShare(theirShare []byte) ([]byte, error) {
	if p.isk != nil {
		return nil, errors.New("duplicate PAKE share")
	}
	y := ristretto255.NewIdentityElement()
	if _, err := y.SetCanonicalBytes(theirShare); err != nil {
		return nil, err
	}
	k := ristretto255.NewIdentityElement().ScalarMult(p.scalar, y)
	if k.Equal(ristretto255.NewIdentityElement()) == 1 {
		return nil, errors.New("degenerate PAKE share")
	}
	hostShare, peerShare := p.share, theirShare
	if p.role == "peer" {
		hostShare, peerShare = theirShare, p.share
	}
	isk := sha512.Sum512(lv([]byte("filedrop-cpace-isk"), []byte(p.hostFp), []byte(p.peerFp),
		hostShare, peerShare, k.Bytes()))
	p.isk = isk[:]
	return p.mac(p.role), nil
}

// VerifyConfirm checks the other side's confirmation MAC.
func (p *Pake) VerifyConfirm(theirMac []byte) bool {
	if p.isk == nil {
		return false
	}
	other := "host"
	if p.role == "host" {
		other = "peer"
	}
	return hmac.Equal(theirMac, p.mac(other))
}

func (p *Pake) mac(label string) []byte {
	m := hmac.New(sha256.New, p.isk)
	m.Write([]byte("filedrop-confirm-" + label))
	return m.Sum(nil)
}

// SplitRoom splits a room code into the signaling topic (first two words) and
// the PAKE secret (the rest), matching parseRoomId() in the web app.
func SplitRoom(room string) (topic, secret string) {
	words := strings.Split(room, "-")
	n := min(2, len(words))
	return strings.Join(words[:n], "-"), strings.Join(words[n:], "-")
}
