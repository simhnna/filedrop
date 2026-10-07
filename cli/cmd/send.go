package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"filedrop-cli/internal/pake"
	"filedrop-cli/internal/peer"
	"filedrop-cli/internal/roomname"
	"filedrop-cli/internal/signaling"
	"filedrop-cli/internal/transfer"
	"filedrop-cli/internal/ui"

	"github.com/spf13/cobra"
)

const (
	signalingURL = "wss://connect.hannaweb.eu"
	webBaseURL   = "https://filedrop.hannaweb.eu"
)

func init() {
	rootCmd.AddCommand(sendCmd)
}

var sendCmd = &cobra.Command{
	Use:   "send <file> [files...]",
	Short: "Send files — creates a room and waits for a receiver",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runSend,
}

func runSend(_ *cobra.Command, args []string) error {
	// Validate all files exist before creating the room.
	for _, path := range args {
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	}

	// Build name→(id, size) for progress bars.  IDs mirror what the transfer
	// manager will assign (sequential from 1).
	type meta struct {
		id   uint32
		size int64
	}
	byName := make(map[string]meta, len(args))
	for i, path := range args {
		st, _ := os.Stat(path)
		byName[filepath.Base(path)] = meta{id: uint32(i + 1), size: st.Size()}
	}

	room := roomname.Generate()
	webURL := webBaseURL + "/r/#" + room
	ui.ShowRoom(room, webURL)
	ui.ShowStatus("Waiting for receiver…")

	done := make(chan error, 1)
	topic, secret := pake.SplitRoom(room)
	sig := signaling.New(signalingURL, "filedrop:"+topic)
	defer sig.Close()

	tm := transfer.NewManager(".", transfer.Callbacks{
		OnSendProgress: func(name string, sent, total int, size int64) {
			if m, ok := byName[name]; ok {
				ui.UpdateProgress(m.id, name, sent, total, size)
			}
		},
		OnAllSent: func() {
			time.Sleep(400 * time.Millisecond)
			done <- nil
		},
	})

	p := peer.New(sig, "host", secret, peer.Events{
		OnConnected: func() {
			ui.ShowStatus("Peer connected — sending…")
			if err := tm.SendFiles(args); err != nil {
				done <- err
			}
		},
		OnDisconnected: func() {
			done <- fmt.Errorf("peer disconnected")
		},
		OnAuthFailed: func() {
			done <- peer.ErrAuthFailed
		},
		OnControl: func(msg map[string]any) { tm.HandleControl(msg) },
		OnData:    func(buf []byte) { tm.HandleData(buf) },
	})
	tm.SetSender(p)
	defer p.Close()

	return <-done
}
