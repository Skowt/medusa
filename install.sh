#!/bin/sh
set -e

# medusa installer script
# Usage: curl -fsSL https://raw.githubusercontent.com/Skowt/medusa/main/install.sh | sh

REPO="Skowt/medusa"
BINARY="medusa"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

# Detect OS
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$OS" in
  darwin) OS="darwin" ;;
  linux) OS="linux" ;;
  *)
    echo "Error: Unsupported operating system: $OS"
    exit 1
    ;;
esac

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *)
    echo "Error: Unsupported architecture: $ARCH"
    exit 1
    ;;
esac

# Get latest version from GitHub API
get_latest_version() {
  curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" |
    grep '"tag_name":' |
    sed -E 's/.*"([^"]+)".*/\1/'
}

VERSION="${VERSION:-$(get_latest_version)}"

if [ -z "$VERSION" ]; then
  echo "Error: Could not determine latest version"
  exit 1
fi

# Remove 'v' prefix if present for filename
VERSION_NUM="${VERSION#v}"

FILENAME="${BINARY}_${VERSION_NUM}_${OS}_${ARCH}.tar.gz"
DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${VERSION}/${FILENAME}"

echo "Installing ${BINARY} ${VERSION} (${OS}/${ARCH})..."

# Create temp directory
TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

# Download and extract
echo "Downloading ${DOWNLOAD_URL}..."
curl -fsSL "$DOWNLOAD_URL" -o "${TMP_DIR}/${FILENAME}"

echo "Extracting..."
tar -xzf "${TMP_DIR}/${FILENAME}" -C "$TMP_DIR"

# Install one binary from TMP_DIR into INSTALL_DIR.
# Usage: install_binary <name> [required]
install_binary() {
  NAME="$1"
  REQUIRED="${2:-optional}"
  SRC="${TMP_DIR}/${NAME}"
  DEST="${INSTALL_DIR}/${NAME}"

  if [ ! -f "$SRC" ]; then
    if [ "$REQUIRED" = "required" ]; then
      echo "Error: expected ${NAME} in archive but it wasn't found"
      exit 1
    fi
    return 0
  fi

  echo "Installing ${NAME} to ${DEST}..."
  if [ -w "$INSTALL_DIR" ]; then
    mv "$SRC" "$DEST"
    chmod +x "$DEST"
  else
    sudo mv "$SRC" "$DEST"
    sudo chmod +x "$DEST"
  fi
}

install_binary "medusa" required
install_binary "medusa-approve-compound"
install_binary "medusa-hook-emit"

echo ""
echo "✓ ${BINARY} ${VERSION} installed successfully!"
echo ""
echo "Run '${BINARY}' to get started."
