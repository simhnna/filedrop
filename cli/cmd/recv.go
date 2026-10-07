package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"filedrop-cli/internal/pake"
	"filedrop-cli/internal/peer"
	"filedrop-cli/internal/roomname"
	"filedrop-cli/internal/signaling"
	"filedrop-cli/internal/transfer"
	"filedrop-cli/internal/ui"

	"github.com/spf13/cobra"
)

var recvOutputDir string

func init() {
	recvCmd.Flags().StringVarP(&recvOutputDir, "output", "o", ".", "Directory to save received files")
	rootCmd.AddCommand(recvCmd)
}

var recvCmd = &cobra.Command{
	Use:   "recv [room code or link]",
	Short: "Receive files — joins a room, or creates one if no code is given",
	Long: `Receive files from a sender.

With a room code or link, joins the sender's room. The code's words can be
separated by dashes or spaces ("filedrop recv word word word word"). Without
one, creates a room and prints its code, link and a QR code so a sender
(e.g. a phone) can join it and send files.`,
	Args: cobra.ArbitraryArgs,
	RunE: runRecv,
}

// fileID → ui bar ID; we use the transfer manager's assigned IDs directly.
func runRecv(_ *cobra.Command, args []string) error {
	// With no code we create the room, so we're the host and create the offer;
	// the browser that joins is the peer and sends.
	role := "host"
	room := roomname.Generate()
	if len(args) > 0 {
		// Unquoted space-separated codes arrive as several args.
		var err error
		if room, err = roomname.Normalize(strings.Join(args, " ")); err != nil {
			return fmt.Errorf("invalid room code: %w", err)
		}
		role = "peer"
	}
	webURL := webBaseURL + "/r/#" + room
	ui.ShowRoom(room, webURL)
	ui.ShowStatus("Waiting for sender…")

	done := make(chan error, 1)
	topic, secret := pake.SplitRoom(room)
	sig := signaling.New(signalingURL, "filedrop:"+topic)
	defer sig.Close()

	// The transfer manager passes file size through the callback, so we don't
	// need to sniff the manifest ourselves.
	idByName := make(map[string]uint32)

	tm := transfer.NewManager(recvOutputDir, transfer.Callbacks{
		OnReceiveProgress: func(name string, received, total int, size int64) {
			id := idByName[name]
			ui.UpdateProgress(id, name, received, total, size)
		},
		OnFileComplete: func(name, finalPath string) {
			ui.FileComplete(idByName[name], name, finalPath)
		},
		OnAllReceived: func() {
			done <- nil
		},
	})
	// Deferred before p.Close so it runs after it, once no more chunks arrive
	defer tm.Cleanup()

	p := peer.New(sig, role, secret, peer.Events{
		OnConnected: func() {
			ui.ShowStatus("Connected — receiving…")
		},
		OnDisconnected: func() {
			done <- fmt.Errorf("peer disconnected")
		},
		OnAuthFailed: func() {
			done <- peer.ErrAuthFailed
		},
		OnControl: func(msg map[string]any) {
			// Populate idByName from the manifest before delegating so that the
			// first OnReceiveProgress call (with received=0) already has a bar ID.
			if msg["type"] == "manifest" {
				if raw, ok := msg["files"].([]any); ok {
					for _, item := range raw {
						f, ok := item.(map[string]any)
						if !ok {
							continue
						}
						name, _ := f["name"].(string)
						idF, _ := f["id"].(float64)
						if name != "" {
							idByName[name] = uint32(idF)
						}
					}
				}
			}
			tm.HandleControl(msg)
		},
		OnData: func(buf []byte) { tm.HandleData(buf) },
	})
	tm.SetSender(p)
	defer p.Close()

	// Catch Ctrl-C so the deferred cleanup runs; a second one exits immediately.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		stop()
		return errors.New("interrupted")
	}
}
