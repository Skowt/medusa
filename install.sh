#!/bin/sh
set -e

# medusa installer script
# Usage: curl -fsSL https://raw.githubusercontent.com/Skowt/medusa/main/install.sh | sh
#
# Installs into a directory you already own. Never asks for a password.
# Override the destination with INSTALL_DIR=/somewhere/else.

REPO="Skowt/medusa"
BINARY="medusa"
DEFAULT_INSTALL_DIR="${HOME}/.local/bin"

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

# Print the first executable named $1 found on PATH, or return 1.
# Resolves PATH by hand rather than using `command -v`, which some shells
# answer from a stale hash table.
first_on_path() {
  _name="$1"
  _saved_ifs="$IFS"
  IFS=:
  for _dir in $PATH; do
    [ -n "$_dir" ] || _dir="."
    if [ -x "${_dir}/${_name}" ]; then
      IFS="$_saved_ifs"
      printf '%s\n' "${_dir}/${_name}"
      return 0
    fi
  done
  IFS="$_saved_ifs"
  return 1
}

# Pick a destination that needs no admin rights:
#   1. An explicit INSTALL_DIR always wins.
#   2. Upgrade in place if medusa already sits in a directory we can write.
#   3. Otherwise ~/.local/bin.
resolve_install_dir() {
  if [ -n "${INSTALL_DIR:-}" ]; then
    printf '%s\n' "$INSTALL_DIR"
    return 0
  fi

  if _existing=$(first_on_path "$BINARY"); then
    _existing_dir=$(dirname "$_existing")
    if [ -w "$_existing_dir" ]; then
      printf '%s\n' "$_existing_dir"
      return 0
    fi
  fi

  if [ -z "${HOME:-}" ]; then
    echo "Error: HOME is not set; re-run with INSTALL_DIR=/path/you/own" >&2
    exit 1
  fi

  printf '%s\n' "$DEFAULT_INSTALL_DIR"
}

INSTALL_DIR=$(resolve_install_dir)

if ! mkdir -p "$INSTALL_DIR" 2>/dev/null || [ ! -w "$INSTALL_DIR" ]; then
  echo "Error: ${INSTALL_DIR} is not writable."
  echo ""
  echo "Install somewhere you own instead:"
  echo "  curl -fsSL https://raw.githubusercontent.com/${REPO}/main/install.sh | INSTALL_DIR=\"\$HOME/.local/bin\" sh"
  exit 1
fi

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
  chmod +x "$SRC"
  # A running medusa holds its binary open; replacing the inode beats writing
  # into it. mv within the same filesystem does that, but TMP_DIR often isn't,
  # so copy to a sibling of the target and rename.
  cp "$SRC" "${DEST}.new"
  mv "${DEST}.new" "$DEST"
}

install_binary "medusa" required
install_binary "medusa-approve-compound"
install_binary "medusa-hook-emit"

echo ""
echo "✓ ${BINARY} ${VERSION} installed successfully!"
echo ""

# Warn if INSTALL_DIR isn't on PATH, or if an older copy earlier on PATH wins.
case ":${PATH}:" in
  *:"${INSTALL_DIR}":*)
    if _found=$(first_on_path "$BINARY") && [ "$_found" != "${INSTALL_DIR}/${BINARY}" ]; then
      echo "Warning: ${_found} comes earlier on your PATH and will shadow this install."
      echo "Remove it, or move ${INSTALL_DIR} ahead of $(dirname "$_found") in PATH."
      echo ""
    fi
    ;;
  *)
    echo "Warning: ${INSTALL_DIR} is not on your PATH. Add it:"
    echo ""
    case "$(basename "${SHELL:-sh}")" in
      fish) echo "  fish_add_path ${INSTALL_DIR}" ;;
      zsh)  echo "  echo 'export PATH=\"${INSTALL_DIR}:\$PATH\"' >> ~/.zshrc && exec zsh" ;;
      *)    echo "  echo 'export PATH=\"${INSTALL_DIR}:\$PATH\"' >> ~/.bashrc && exec bash" ;;
    esac
    echo ""
    ;;
esac

echo "Run '${BINARY}' to get started."
