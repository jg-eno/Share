# Share

A secure, self-hosted file sharing tool for your personal devices.

Share exposes a directory from your machine over the local network (or beyond) with a clean web UI — no cloud, no accounts, no passwords. New devices are authenticated with public-key cryptography: each browser generates an ECDSA key pair, and you approve connections directly from the terminal.

---

## Features

- **Passwordless device authentication** — browsers generate an ECDSA P-256 key pair on first visit; you approve each device from the TUI with a single keypress
- **Interactive terminal UI** — built with Bubble Tea; includes a directory picker, live transfer logs, and an approval alert banner
- **Modern web interface** — responsive grid/list file browser with drag-and-drop upload, breadcrumb navigation, and search
- **Secure by default** — all file endpoints require a valid session cookie; path traversal is blocked server-side
- **QR code pairing** — scan to open the server URL on any device instantly
- **Persistent device store** — approved devices saved to `~/.share_devices.json` and reloaded on restart
- **Directory browsing and downloads** — browse nested folders and download any file
- **Upload support** — drag and drop or click to upload files up to 10 GB

---

## Installation

### One-line install (Linux / macOS)

```bash
curl -fsSL https://raw.githubusercontent.com/glenenosh/share/main/install.sh | bash
```

The script auto-detects your OS and architecture, downloads the appropriate pre-built binary from GitHub Releases, and installs it to `/usr/local/bin`. If no binary is available for your platform it falls back to building from source (requires Go 1.21+).

### Build from source

```bash
git clone https://github.com/glenenosh/share.git
cd share
go build -o share .
sudo mv share /usr/local/bin/
```

---

## Usage

### Start the server interactively

```bash
share serve
```

Launches a terminal picker — use the arrow keys to navigate to the folder you want to share, then press `s` to start the server.

### Share a specific directory

```bash
share serve -d ./Documents
```

Starts the server immediately on the default port (15016) sharing `./Documents`.

### List registered devices

```bash
share devices
```

Prints a table of all devices that have ever registered with this server, including their approval status, authentication mode (ECDSA or simple), and registration date.

Example output:

```
   SHARE — Registered Devices

  NAME                      STATUS        MODE        REGISTERED
  ────────────────────────────────────────────────────────────────
  My iPhone                 ✓ approved    ECDSA       2026-06-01
  Work Laptop               ✓ approved    simple      2026-06-15
  iPad Pro                  ⏳ pending     ECDSA       2026-06-30

  3 device(s) total  —  2 approved  1 pending
```

### Revoke device access

```bash
share revoke
```

Opens an interactive selector listing all approved devices. Use arrow keys to navigate, `Space` to toggle selection, and `Enter` to confirm. You will be asked for a final confirmation before any changes are made.

Revoked devices are removed from `~/.share_devices.json`. They will need to re-register and be re-approved on their next visit.

### Options

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--dir` | `-d` | `.` | Directory to share |
| `--port` | `-p` | `15016` | HTTP port to listen on |

---

## Connecting a device

1. Run `share serve` on your machine. The TUI displays the server address and a **scannable QR code** directly in the terminal.
2. Scan the QR code with your phone/tablet camera, or open the URL manually in any browser (or use the **Connect Device** button in the web UI for a larger QR).
3. Enter a name for the device (e.g. "My Phone") and click **Request Access**.
4. Back in the terminal TUI you'll see a red alert banner:

   ```
   ⚠  APPROVAL REQUIRED — Device "My Phone" wants to connect.
      Press [a] to Approve | [r] to Reject
   ```

5. Press `a` to approve. The browser automatically completes the challenge-response handshake and unlocks the file browser.

Approved devices are remembered across server restarts. Subsequent visits skip the approval step and authenticate silently.

---

## TUI Key Bindings

**Picker mode**

| Key | Action |
|-----|--------|
| `↑` / `↓` | Navigate entries |
| `Enter` / `→` | Enter directory |
| `Backspace` / `←` | Go to parent |
| `s` | Select current directory and start server |
| `q` / `Ctrl+C` | Quit |

**Monitor mode**

| Key | Action |
|-----|--------|
| `a` | Approve the oldest pending device |
| `r` | Reject the oldest pending device |
| `q` / `Ctrl+C` | Gracefully shut down the server |

---

## How authentication works

The auth strategy adapts to the browser's security context. In both cases, **the server owner must explicitly approve every new device** from the terminal — that approval is the trust gate.

### Secure context (HTTPS or localhost) — full ECDSA flow

1. On first visit the browser generates an **ECDSA P-256 key pair** using the Web Cryptography API. The private key is stored in `localStorage` as a JWK; the public key is sent to the server in SPKI format.
2. The server queues the device as **pending** and fires an approval notification to the TUI.
3. A red alert banner appears in the terminal:
   ```
   ⚠  APPROVAL REQUIRED — Device "My Phone" wants to connect.
      Press [a] to Approve | [r] to Reject
   ```
4. Once you press `a`, the device is marked **approved** and persisted to `~/.share_devices.json`.
5. The browser polls `/api/auth/status` until approved, then fetches a one-time **challenge nonce** from `/api/auth/challenge`.
6. The nonce is signed with the private key using ECDSA P-256 + SHA-256 and submitted to `/api/auth/verify`.
7. The server verifies the signature cryptographically. On success it sets a `share_session` cookie (valid for 30 days). All file endpoints require this cookie.

Subsequent visits from the same device skip steps 1–6 entirely — the stored key pair re-authenticates silently with a fresh challenge.

### Insecure context (plain HTTP over LAN) — simple token flow

Browsers block `crypto.subtle` on plain `http://` connections to non-`localhost` origins (e.g. `http://192.168.1.5:15016`). In this case:

1. The browser generates a **random 48-character hex device ID** using `crypto.getRandomValues` (available in all contexts) and stores it in `localStorage`.
2. Registration and TUI approval proceed identically to the secure flow.
3. Once approved, the browser calls `/api/auth/verify-simple` with just the device ID — no signature required.
4. The server checks that the device ID is in the approved list and issues the session cookie.

The security trade-off: without a cryptographic signature, a session cannot be cryptographically bound to the device that registered. However, the explicit TUI approval step still ensures no device gains access without the server owner's knowledge.

> To get the full ECDSA flow on a LAN, serve Share behind a reverse proxy with a TLS certificate, or access it via `localhost` on the host machine.

### What is and isn't stored

| Item | Where | Notes |
|------|-------|-------|
| Private key (secure mode) | Browser `localStorage` (JWK) | Never sent to the server |
| Device ID (simple mode) | Browser `localStorage` | Random hex, no key material |
| Approved devices | `~/.share_devices.json` | Name, public key / device ID, status, date |
| Active sessions | Server memory only | Cleared on server restart |

---

## Project structure

```
share/
├── cmd/
│   ├── root.go          # Cobra root command
│   └── serve.go         # serve subcommand
├── internal/
│   ├── network/
│   │   └── ip.go        # Local IP detection
│   └── server/
│       ├── auth.go      # Device store, signature verification
│       ├── file_server.go  # HTTP handlers and auth endpoints
│       ├── server.go    # Server struct and helpers
│       ├── tui.go       # Bubble Tea terminal UI
│       ├── web.go       # Embedded static assets
│       ├── auth_test.go
│       ├── server_test.go
│       └── web/
│           ├── app.js
│           ├── index.html
│           └── style.css
├── install.sh
├── main.go
└── go.mod
```

---

## License

MIT
