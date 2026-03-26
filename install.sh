#!/bin/sh
set -e

REPO="ringclaw/ringclaw"
BINARY="ringclaw"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
CHANNEL="${1:-}" # alpha, beta, or empty for stable

# Detect OS
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$OS" in
  darwin|linux) ;;
  *) echo "Unsupported OS: $OS"; exit 1 ;;
esac

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

echo "Detected: ${OS}/${ARCH}"

if [ "$CHANNEL" = "beta" ]; then
  # Beta: latest build from main branch
  VERSION="beta-latest"
  echo "Channel: beta (latest main build)"
elif [ "$CHANNEL" = "alpha" ]; then
  # Alpha: specify branch name as second arg, default to latest alpha
  BRANCH="${2:-}"
  if [ -n "$BRANCH" ]; then
    SAFE_BRANCH=$(echo "$BRANCH" | sed 's/[^a-zA-Z0-9._-]/-/g')
    VERSION="alpha-${SAFE_BRANCH}"
    echo "Channel: alpha (branch: ${BRANCH})"
  else
    echo "Usage: install.sh alpha <branch-name>"
    echo "Example: install.sh alpha feature/tasks-notes-events"
    exit 1
  fi
else
  # Stable: latest tagged release
  echo "Fetching latest release..."
  VERSION=$(curl -fsSI "https://github.com/${REPO}/releases/latest" 2>/dev/null | grep -i '^location:' | sed 's|.*/tag/||' | tr -d '\r\n')
  if [ -z "$VERSION" ]; then
    echo "Error: could not determine latest version. Is there a release on GitHub?"
    exit 1
  fi
  echo "Channel: stable"
fi

echo "Version: ${VERSION}"

# Download
FILENAME="${BINARY}_${OS}_${ARCH}"
URL="https://github.com/${REPO}/releases/download/${VERSION}/${FILENAME}"

echo "Downloading ${URL}..."
TMP=$(mktemp)
curl -fsSL -o "$TMP" "$URL"

# Install
chmod +x "$TMP"
if [ -d "$INSTALL_DIR" ] && [ -w "$INSTALL_DIR" ]; then
  mv "$TMP" "${INSTALL_DIR}/${BINARY}"
else
  echo "Installing to ${INSTALL_DIR} (requires sudo)..."
  sudo mkdir -p "$INSTALL_DIR"
  sudo mv "$TMP" "${INSTALL_DIR}/${BINARY}"
fi

# Clear macOS quarantine attributes (ported from weclaw c1d5e12)
if [ "$OS" = "darwin" ]; then
  xattr -d com.apple.quarantine "${INSTALL_DIR}/${BINARY}" 2>/dev/null || true
  xattr -d com.apple.provenance "${INSTALL_DIR}/${BINARY}" 2>/dev/null || true
fi

echo ""
echo "ringclaw ${VERSION} installed to ${INSTALL_DIR}/${BINARY}"
echo ""
echo "Usage:"
echo "  ringclaw start"
echo ""
echo "Install channels:"
echo "  install.sh              # stable (latest tag)"
echo "  install.sh beta         # beta (latest main build)"
echo "  install.sh alpha <branch>  # alpha (specific branch)"
