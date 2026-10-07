# filedrop

Peer-to-peer file transfer in the browser and on the command line. Files go
directly between devices over WebRTC — no upload, no account.

**Live:** <https://filedrop.hannaweb.eu>

## How it works

1. Create a room. You get a four-word code like `oak-hike-salt-blue` (plus a
   link and a QR code).
2. Share the code with the receiver. They open the link, scan the QR code or
   type the code.
3. Drop files or text in. They stream straight to the other device.

- **End-to-end encrypted.** Transfers run over WebRTC data channels (DTLS).
  Both sides prove they know the room code with a PAKE bound to the DTLS
  fingerprints, so even the signaling server can't sit in the middle. Only the
  first two words of the code ever leave your device; the rest is the secret.
- **The code stays private.** It lives in the URL fragment (`/r/#…`), which
  browsers never send to servers.
- **Resumable.** Received chunks are written to the browser's Origin Private
  File System. If the connection drops, re-offering the same file picks up
  where it left off. Unfinished data is cleaned up after 7 days.
- **Multiple receivers.** The room creator can send to several peers at once.
- **Ask before receiving.** By default each incoming file needs to be accepted;
  turn that off to accept everything automatically.

## CLI

For headless machines, a Go CLI talks to the same rooms as the web app.

Install on macOS or Linux:

```sh
curl -fsSL https://filedrop.hannaweb.eu/install.sh | sh
```

Set `INSTALL_DIR` to change the destination (default `~/.local/bin`). Windows
and other builds are on the [releases page](https://github.com/simhnna/filedrop/releases/latest).

```sh
filedrop send report.pdf photos.zip   # creates a room, prints code + QR, waits
filedrop recv oak-hike-salt-blue      # joins a room (code or pasted link)
filedrop recv                         # creates a room, prints code + QR, waits for a sender
filedrop recv <code> -o ~/Downloads   # save somewhere other than the cwd
```

Either side can be the browser — e.g. `filedrop send` on a server and open the
printed link on your phone. Existing files are never overwritten; duplicates
are saved as `name (1).ext`, `name (2).ext`, … The CLI doesn't resume
interrupted transfers yet.

## Development

Requires Node (see `.node-version`) with pnpm, and Go (see `cli/go.mod`) for
the CLI.

```sh
pnpm install
pnpm dev       # dev server on http://localhost:5173
pnpm check     # svelte-check + tsc
pnpm build     # production build → dist/
pnpm preview   # serve dist/ locally
```

```sh
cd cli
go build .     # → ./filedrop
go test ./...
```

The web app is Svelte 5 + TypeScript + Vite (no SvelteKit). Signaling goes
through an external y-webrtc pub/sub hub at `wss://connect.hannaweb.eu`.
See [CLAUDE.md](CLAUDE.md) for the connection, authentication and transfer
protocols in detail. The web app and CLI implement the same protocol and must
stay compatible.

## Deployment

The site is static and deployed on Cloudflare Workers static assets
([wrangler.jsonc](wrangler.jsonc)). Build with `pnpm build` and serve `dist/`.
Any other host works as long as it serves `index.html` for unknown paths
(at least `/r/`), since routing is client-side.

## Releasing the CLI

Push a `v*` tag (e.g. `v1.2.3`). The [release workflow](.github/workflows/release.yml)
cross-compiles for Linux, macOS and Windows (amd64 + arm64), publishes a GitHub
release with `checksums.txt`, and `install.sh` picks up the latest one.
