#!/bin/bash
# Share — Universal Installer
#
# One-line install:
#   curl -fsSL https://raw.githubusercontent.com/jg-eno/Share/main/install.sh | bash
#
# Downloads a pre-built binary for your OS/arch from GitHub Releases,
# verifies it, and installs it to /usr/local/bin (or ~/bin as fallback).
# Falls back to building from source if no binary is available.

set -euo pipefail

# ── Configuration ────────────────────────────────────────────────────────────
REPO="jg-eno/Share"
BINARY_NAME="share"
INSTALL_DIR="/usr/local/bin"
FALLBACK_DIR="$HOME/.local/bin"
GITHUB_API="https://api.github.com/repos/${REPO}/releases/latest"
# ─────────────────────────────────────────────────────────────────────────────

RED='\033[0;31m'
GREEN='\033[0;32m'
CYAN='\033[0;36m'
BOLD='\033[1m'
RESET='\033[0m'

info()    { echo -e "${CYAN}${BOLD}[share]${RESET} $*"; }
success() { echo -e "${GREEN}${BOLD}[share]${RESET} $*"; }
error()   { echo -e "${RED}${BOLD}[share] Error:${RESET} $*" >&2; }
die()     { error "$*"; exit 1; }

# ── Detect OS and architecture ───────────────────────────────────────────────

detect_os() {
    case "$(uname -s)" in
        Linux*)  echo "linux"  ;;
        Darwin*) echo "darwin" ;;
        MINGW*|MSYS*|CYGWIN*) echo "windows" ;;
        *) die "Unsupported operating system: $(uname -s)" ;;
    esac
}

detect_arch() {
    case "$(uname -m)" in
        x86_64|amd64)   echo "amd64"  ;;
        aarch64|arm64)  echo "arm64"  ;;
        armv7l)         echo "armv7"  ;;
        i386|i686)      echo "386"    ;;
        *) die "Unsupported architecture: $(uname -m)" ;;
    esac
}

# ── Check for required tools ──────────────────────────────────────────────────

require_cmd() {
    if ! command -v "$1" &>/dev/null; then
        die "Required command not found: $1. Please install it and try again."
    fi
}

# ── Fetch the latest release tag from GitHub ─────────────────────────────────

get_latest_version() {
    if command -v curl &>/dev/null; then
        curl -fsSL "$GITHUB_API" 2>/dev/null \
            | grep '"tag_name"' \
            | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/'
    elif command -v wget &>/dev/null; then
        wget -qO- "$GITHUB_API" 2>/dev/null \
            | grep '"tag_name"' \
            | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/'
    else
        die "Neither curl nor wget is available. Please install one and retry."
    fi
}

# ── Download a file ───────────────────────────────────────────────────────────

download() {
    local url="$1"
    local dest="$2"
    if command -v curl &>/dev/null; then
        curl -fsSL --progress-bar "$url" -o "$dest"
    else
        wget -q --show-progress "$url" -O "$dest"
    fi
}

# ── Install binary to a writable directory ────────────────────────────────────

install_binary() {
    local src="$1"
    chmod +x "$src"

    if [ -w "$INSTALL_DIR" ]; then
        mv "$src" "${INSTALL_DIR}/${BINARY_NAME}"
        echo "$INSTALL_DIR"
    elif sudo -n true 2>/dev/null; then
        sudo mv "$src" "${INSTALL_DIR}/${BINARY_NAME}"
        echo "$INSTALL_DIR"
    else
        info "No write access to ${INSTALL_DIR}. Installing to ${FALLBACK_DIR} instead."
        mkdir -p "$FALLBACK_DIR"
        mv "$src" "${FALLBACK_DIR}/${BINARY_NAME}"
        # Remind the user if the fallback dir is not in PATH
        if [[ ":$PATH:" != *":${FALLBACK_DIR}:"* ]]; then
            echo ""
            info "Add this to your shell profile to use 'share' globally:"
            echo -e "  ${BOLD}export PATH=\"\$HOME/.local/bin:\$PATH\"${RESET}"
        fi
        echo "$FALLBACK_DIR"
    fi
}

# ── Build from source (fallback) ──────────────────────────────────────────────

build_from_source() {
    info "No pre-built binary available — building from source..."
    require_cmd go

    local tmpdir
    tmpdir="$(mktemp -d)"
    trap "rm -rf $tmpdir" EXIT

    if command -v git &>/dev/null; then
        git clone --depth=1 "https://github.com/${REPO}.git" "$tmpdir/share"
        ( cd "$tmpdir/share" && go build -ldflags="-s -w" -o "$tmpdir/${BINARY_NAME}" . )
    else
        die "git is not installed. Cannot build from source."
    fi

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

    local os arch version
    os="$(detect_os)"
    arch="$(detect_arch)"

    info "Detected platform: ${os}/${arch}"
    info "Fetching latest release..."

    version="$(get_latest_version)"
    if [ -z "$version" ]; then
        info "Could not determine latest release. Falling back to source build."
        build_from_source
        return
    fi

    info "Latest version: ${version}"

    # Construct the asset filename — e.g. share_linux_amd64
    local asset_name="${BINARY_NAME}_${os}_${arch}"
    if [ "$os" = "windows" ]; then
        asset_name="${asset_name}.exe"
    fi

    local download_url="https://github.com/${REPO}/releases/download/${version}/${asset_name}"
    local tmpdir tmpbin
    tmpdir="$(mktemp -d)"
    tmpbin="${tmpdir}/${BINARY_NAME}"
    trap "rm -rf $tmpdir" EXIT

    info "Downloading ${asset_name}..."
    if ! download "$download_url" "$tmpbin" 2>/dev/null; then
        info "Pre-built binary not found for ${os}/${arch}. Falling back to source build."
        rm -rf "$tmpdir"
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
