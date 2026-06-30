#!/bin/bash
# Share — Universal Installer
#
# One-line install:
#   curl -fsSL https://raw.githubusercontent.com/jg-eno/Share/main/install.sh | bash
#
# Primary path  : downloads a pre-built binary from GitHub Releases (no Go needed).
# Fallback path : builds from source — auto-installs Go if not present.

set -euo pipefail

# ── Configuration ─────────────────────────────────────────────────────────────
REPO="jg-eno/Share"
BINARY_NAME="share"
INSTALL_DIR="/usr/local/bin"
FALLBACK_DIR="$HOME/.local/bin"
GITHUB_API="https://api.github.com/repos/${REPO}/releases/latest"
GO_VERSION="1.22.4"   # minimum Go version used when auto-installing
# ─────────────────────────────────────────────────────────────────────────────

RED='\033[0;31m'
GREEN='\033[0;32m'
CYAN='\033[0;36m'
YELLOW='\033[0;33m'
BOLD='\033[1m'
RESET='\033[0m'

info()    { echo -e "${CYAN}${BOLD}[share]${RESET} $*"; }
success() { echo -e "${GREEN}${BOLD}[share]${RESET} $*"; }
warn()    { echo -e "${YELLOW}${BOLD}[share]${RESET} $*"; }
error()   { echo -e "${RED}${BOLD}[share] Error:${RESET} $*" >&2; }
die()     { error "$*"; exit 1; }

# ── Platform detection ────────────────────────────────────────────────────────

detect_os() {
    case "$(uname -s)" in
        Linux*)             echo "linux"   ;;
        Darwin*)            echo "darwin"  ;;
        MINGW*|MSYS*|CYGWIN*) echo "windows" ;;
        *) die "Unsupported operating system: $(uname -s)" ;;
    esac
}

detect_arch() {
    case "$(uname -m)" in
        x86_64|amd64)  echo "amd64"  ;;
        aarch64|arm64) echo "arm64"  ;;
        armv7l)        echo "armv7"  ;;
        i386|i686)     echo "386"    ;;
        *) die "Unsupported architecture: $(uname -m)" ;;
    esac
}

# ── Download helper ───────────────────────────────────────────────────────────

download() {
    local url="$1" dest="$2"
    if command -v curl &>/dev/null; then
        curl -fsSL --progress-bar "$url" -o "$dest"
    elif command -v wget &>/dev/null; then
        wget -q --show-progress "$url" -O "$dest"
    else
        die "Neither curl nor wget is available. Please install one and retry."
    fi
}

download_silent() {
    local url="$1" dest="$2"
    if command -v curl &>/dev/null; then
        curl -fsSL "$url" -o "$dest"
    else
        wget -qO "$dest" "$url"
    fi
}

fetch_text() {
    if command -v curl &>/dev/null; then
        curl -fsSL "$1" 2>/dev/null
    else
        wget -qO- "$1" 2>/dev/null
    fi
}

# ── GitHub release version ────────────────────────────────────────────────────

get_latest_version() {
    fetch_text "$GITHUB_API" \
        | grep '"tag_name"' \
        | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/'
}

# ── Binary installation ───────────────────────────────────────────────────────

install_binary() {
    local src="$1"
    chmod +x "$src"

    if [ -w "$INSTALL_DIR" ]; then
        mv "$src" "${INSTALL_DIR}/${BINARY_NAME}"
        echo "$INSTALL_DIR"
    elif command -v sudo &>/dev/null && sudo -n true 2>/dev/null; then
        sudo mv "$src" "${INSTALL_DIR}/${BINARY_NAME}"
        echo "$INSTALL_DIR"
    elif command -v sudo &>/dev/null; then
        info "Root access needed to install to ${INSTALL_DIR}."
        sudo mv "$src" "${INSTALL_DIR}/${BINARY_NAME}"
        echo "$INSTALL_DIR"
    else
        warn "No write access to ${INSTALL_DIR}. Installing to ${FALLBACK_DIR}."
        mkdir -p "$FALLBACK_DIR"
        mv "$src" "${FALLBACK_DIR}/${BINARY_NAME}"
        if [[ ":$PATH:" != *":${FALLBACK_DIR}:"* ]]; then
            echo ""
            warn "Add this to your shell profile so 'share' is in PATH:"
            echo -e "  ${BOLD}export PATH=\"\$HOME/.local/bin:\$PATH\"${RESET}"
        fi
        echo "$FALLBACK_DIR"
    fi
}

# ── Go auto-installer ─────────────────────────────────────────────────────────

install_go() {
    local os arch go_arch go_tarball go_url tmpdir

    os="$(detect_os)"
    arch="$(detect_arch)"

    # Map arch names to Go's naming
    case "$arch" in
        amd64) go_arch="amd64"  ;;
        arm64) go_arch="arm64"  ;;
        armv7) go_arch="armv6l" ;;
        386)   go_arch="386"    ;;
        *) die "Cannot auto-install Go for architecture: $arch" ;;
    esac

    if [ "$os" = "darwin" ]; then
        # Prefer Homebrew on macOS — much simpler
        if command -v brew &>/dev/null; then
            info "Installing Go via Homebrew..."
            brew install go
            return
        fi
    fi

    if [ "$os" = "linux" ]; then
        # Try the system package manager first
        if command -v apt-get &>/dev/null; then
            info "Installing Go via apt..."
            sudo apt-get update -qq && sudo apt-get install -y golang-go
            return
        elif command -v dnf &>/dev/null; then
            info "Installing Go via dnf..."
            sudo dnf install -y golang
            return
        elif command -v pacman &>/dev/null; then
            info "Installing Go via pacman..."
            sudo pacman -Sy --noconfirm go
            return
        elif command -v apk &>/dev/null; then
            info "Installing Go via apk..."
            sudo apk add --no-cache go
            return
        fi
    fi

    # Universal fallback: download the official Go tarball
    info "Downloading Go ${GO_VERSION} from golang.org..."
    go_tarball="go${GO_VERSION}.${os}-${go_arch}.tar.gz"
    go_url="https://go.dev/dl/${go_tarball}"
    tmpdir="$(mktemp -d)"

    download "$go_url" "${tmpdir}/${go_tarball}"

    info "Installing Go to /usr/local/go..."
    if [ -d /usr/local/go ]; then
        sudo rm -rf /usr/local/go
    fi
    sudo tar -C /usr/local -xzf "${tmpdir}/${go_tarball}"
    rm -rf "$tmpdir"

    # Make go available in the current shell session
    export PATH="/usr/local/go/bin:$PATH"

    # Persist to common shell profiles
    for profile in "$HOME/.bashrc" "$HOME/.zshrc" "$HOME/.profile"; do
        if [ -f "$profile" ] && ! grep -q '/usr/local/go/bin' "$profile"; then
            echo 'export PATH="/usr/local/go/bin:$PATH"' >> "$profile"
        fi
    done

    info "Go installed: $(go version)"
}

# ── Build from source ─────────────────────────────────────────────────────────

build_from_source() {
    info "No pre-built binary found — attempting to build from source..."

    # Auto-install Go if missing
    if ! command -v go &>/dev/null; then
        warn "Go is not installed."
        echo ""
        echo -e "  ${BOLD}Share will attempt to install Go automatically.${RESET}"
        echo -e "  You can also install it manually: ${CYAN}https://go.dev/doc/install${RESET}"
        echo ""
        read -r -p "  Install Go automatically? [y/N] " yn
        case "$yn" in
            [Yy]*) install_go ;;
            *)
                echo ""
                error "Go is required to build from source."
                echo ""
                echo -e "  Install Go from: ${CYAN}https://go.dev/doc/install${RESET}"
                echo ""
                echo -e "  Or download a pre-built Share binary directly from:"
                echo -e "  ${CYAN}https://github.com/${REPO}/releases${RESET}"
                echo ""
                exit 1
                ;;
        esac
    fi

    if ! command -v git &>/dev/null; then
        die "git is not installed. Install git or download a binary from https://github.com/${REPO}/releases"
    fi

    local tmpdir
    tmpdir="$(mktemp -d)"
    # shellcheck disable=SC2064
    trap "rm -rf $tmpdir" EXIT

    info "Cloning repository..."
    git clone --depth=1 "https://github.com/${REPO}.git" "$tmpdir/share"

    info "Building binary..."
    ( cd "$tmpdir/share" && go build -ldflags="-s -w" -o "$tmpdir/${BINARY_NAME}" . )

    local installed_dir
    installed_dir="$(install_binary "$tmpdir/${BINARY_NAME}")"
    success "Built and installed to ${installed_dir}/${BINARY_NAME}"
}

# ── Main ──────────────────────────────────────────────────────────────────────

main() {
    echo ""
    echo -e "${BOLD}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${RESET}"
    echo -e "${BOLD}  Share — Secure Local File Exchange${RESET}"
    echo -e "${BOLD}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${RESET}"
    echo ""

    local os arch
    os="$(detect_os)"
    arch="$(detect_arch)"

    info "Detected platform: ${os}/${arch}"
    info "Fetching latest release..."

    local version
    version="$(get_latest_version)"

    if [ -z "$version" ]; then
        warn "Could not reach GitHub API. Falling back to source build."
        build_from_source
        return
    fi

    info "Latest version: ${version}"

    # Construct asset filename — e.g. share_linux_amd64
    local asset_name="${BINARY_NAME}_${os}_${arch}"
    [ "$os" = "windows" ] && asset_name="${asset_name}.exe"

    local download_url="https://github.com/${REPO}/releases/download/${version}/${asset_name}"
    local tmpdir tmpbin
    tmpdir="$(mktemp -d)"
    tmpbin="${tmpdir}/${BINARY_NAME}"
    # shellcheck disable=SC2064
    trap "rm -rf $tmpdir" EXIT

    info "Downloading ${asset_name}..."
    if ! download "$download_url" "$tmpbin" 2>/dev/null; then
        warn "Pre-built binary not found for ${os}/${arch}."
        rm -f "$tmpbin"
        build_from_source
        return
    fi

    local installed_dir
    installed_dir="$(install_binary "$tmpbin")"

    echo ""
    success "Installed share ${version} → ${installed_dir}/${BINARY_NAME}"
    echo ""
    info "Get started:"
    echo -e "  ${BOLD}share serve${RESET}            # pick a folder interactively"
    echo -e "  ${BOLD}share serve -d ./docs${RESET}  # share a specific directory"
    echo ""
}

main "$@"
