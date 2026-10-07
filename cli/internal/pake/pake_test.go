package pake

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"os"
	"testing"
)

// The vectors are shared with src/lib/pake.test.ts, which checks that the web
// app derives the same shares and MACs. Run `go test ./internal/pake -update`
// to regenerate the expected values after an intentional protocol change.
const vectorsPath = "../../../testdata/pake.json"

var update = flag.Bool("update", false, "rewrite expected values in testdata/pake.json")

type pakeCase struct {
	Name      string `json:"name"`
	Secret    string `json:"secret"`
	HostFp    string `json:"hostFp"`
	PeerFp    string `json:"peerFp"`
	HostSeed  string `json:"hostSeed"`
	PeerSeed  string `json:"peerSeed"`
	HostShare string `json:"hostShare"`
	PeerShare string `json:"peerShare"`
	HostMac   string `json:"hostMac"`
	PeerMac   string `json:"peerMac"`
}

type fingerprintCase struct {
	Name string `json:"name"`
	SDP  string `json:"sdp"`
	Want string `json:"want"`
}

type vectors struct {
	Cases        []pakeCase        `json:"cases"`
	Fingerprints []fingerprintCase `json:"fingerprints"`
}

func loadVectors(t *testing.T) vectors {
	t.Helper()
	b, err := os.ReadFile(vectorsPath)
	if err != nil {
		t.Fatal(err)
	}
	var v vectors
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func seeded(t *testing.T, hexSeed string) *bytes.Reader {
	t.Helper()
	b, err := hex.DecodeString(hexSeed)
	if err != nil || len(b) != 64 {
		t.Fatalf("bad seed %q", hexSeed)
	}
	return bytes.NewReader(b)
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func TestVectors(t *testing.T) {
	v := loadVectors(t)

	for i := range v.Cases {
		c := &v.Cases[i]
		t.Run(c.Name, func(t *testing.T) {
			host := newWithRand(c.Secret, "host", c.HostFp, c.PeerFp, seeded(t, c.HostSeed))
			peer := newWithRand(c.Secret, "peer", c.HostFp, c.PeerFp, seeded(t, c.PeerSeed))
			hostMac, err := host.ReceiveShare(peer.Share())
			if err != nil {
				t.Fatal(err)
			}
			peerMac, err := peer.ReceiveShare(host.Share())
			if err != nil {
				t.Fatal(err)
			}
			if !host.VerifyConfirm(peerMac) || !peer.VerifyConfirm(hostMac) {
				t.Fatal("confirmation failed")
			}

			got := pakeCase{
				HostShare: b64(host.Share()), PeerShare: b64(peer.Share()),
				HostMac: b64(hostMac), PeerMac: b64(peerMac),
			}
			if *update {
				c.HostShare, c.PeerShare, c.HostMac, c.PeerMac = got.HostShare, got.PeerShare, got.HostMac, got.PeerMac
				return
			}
			for _, f := range []struct{ name, got, want string }{
				{"hostShare", got.HostShare, c.HostShare},
				{"peerShare", got.PeerShare, c.PeerShare},
				{"hostMac", got.HostMac, c.HostMac},
				{"peerMac", got.PeerMac, c.PeerMac},
			} {
				if f.got != f.want {
					t.Errorf("%s = %s, want %s", f.name, f.got, f.want)
				}
			}
		})
	}

	for i := range v.Fingerprints {
		c := &v.Fingerprints[i]
		t.Run("fingerprints/"+c.Name, func(t *testing.T) {
			got := SDPFingerprints(c.SDP)
			if *update {
				c.Want = got
			} else if got != c.Want {
				t.Errorf("SDPFingerprints = %q, want %q", got, c.Want)
			}
		})
	}

	if *update {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(v); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(vectorsPath, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// pair runs a full exchange and reports whether each side accepted the other.
func pair(t *testing.T, host, peer *Pake) (hostOK, peerOK bool) {
	t.Helper()
	hostMac, err := host.ReceiveShare(peer.Share())
	if err != nil {
		t.Fatal(err)
	}
	peerMac, err := peer.ReceiveShare(host.Share())
	if err != nil {
		t.Fatal(err)
	}
	return host.VerifyConfirm(peerMac), peer.VerifyConfirm(hostMac)
}

func TestExchange(t *testing.T) {
	const hostFp, peerFp = "sha-256 AA", "sha-256 BB"

	t.Run("matching secret", func(t *testing.T) {
		h, p := pair(t, New("a-b", "host", hostFp, peerFp), New("a-b", "peer", hostFp, peerFp))
		if !h || !p {
			t.Fatalf("host=%v peer=%v, want both true", h, p)
		}
	})
	t.Run("wrong secret", func(t *testing.T) {
		h, p := pair(t, New("a-b", "host", hostFp, peerFp), New("a-c", "peer", hostFp, peerFp))
		if h || p {
			t.Fatalf("host=%v peer=%v, want both false", h, p)
		}
	})
	t.Run("swapped fingerprint (MITM)", func(t *testing.T) {
		h, p := pair(t, New("a-b", "host", hostFp, peerFp), New("a-b", "peer", hostFp, "sha-256 CC"))
		if h || p {
			t.Fatalf("host=%v peer=%v, want both false", h, p)
		}
	})
	t.Run("same role on both sides", func(t *testing.T) {
		h, p := pair(t, New("a-b", "host", hostFp, peerFp), New("a-b", "host", hostFp, peerFp))
		if h || p {
			t.Fatalf("reflected MAC accepted: host=%v peer=%v", h, p)
		}
	})
}

func TestReceiveShareRejects(t *testing.T) {
	other := New("x", "peer", "", "").Share()

	t.Run("duplicate", func(t *testing.T) {
		p := New("x", "host", "", "")
		if _, err := p.ReceiveShare(other); err != nil {
			t.Fatal(err)
		}
		if _, err := p.ReceiveShare(other); err == nil {
			t.Fatal("second share accepted")
		}
	})
	t.Run("identity", func(t *testing.T) {
		if _, err := New("x", "host", "", "").ReceiveShare(make([]byte, 32)); err == nil {
			t.Fatal("identity element accepted")
		}
	})
	t.Run("non-canonical", func(t *testing.T) {
		bad := bytes.Repeat([]byte{0xff}, 32)
		if _, err := New("x", "host", "", "").ReceiveShare(bad); err == nil {
			t.Fatal("non-canonical encoding accepted")
		}
	})
	t.Run("wrong length", func(t *testing.T) {
		if _, err := New("x", "host", "", "").ReceiveShare(other[:31]); err == nil {
			t.Fatal("short share accepted")
		}
	})
	t.Run("verify before share", func(t *testing.T) {
		if New("x", "host", "", "").VerifyConfirm(make([]byte, 32)) {
			t.Fatal("verified without a key")
		}
	})
}

func TestSplitRoom(t *testing.T) {
	for _, c := range []struct{ room, topic, secret string }{
		{"acid-acorn-acre-acts", "acid-acorn", "acre-acts"},
		{"acid-acorn", "acid-acorn", ""},
		{"acid", "acid", ""},
	} {
		topic, secret := SplitRoom(c.room)
		if topic != c.topic || secret != c.secret {
			t.Errorf("SplitRoom(%q) = %q, %q; want %q, %q", c.room, topic, secret, c.topic, c.secret)
		}
	}
}

func TestSplitRoomVectors(t *testing.T) {
	b, err := os.ReadFile("../../../testdata/compat.json")
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		SplitRoom []struct{ RoomID, Topic, Secret string } `json:"splitRoom"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	for _, c := range v.SplitRoom {
		topic, secret := SplitRoom(c.RoomID)
		if topic != c.Topic || secret != c.Secret {
			t.Errorf("SplitRoom(%q) = %q, %q; want %q, %q", c.RoomID, topic, secret, c.Topic, c.Secret)
		}
	}
}
