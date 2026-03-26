#!/bin/sh
set -e

# Devbox installer
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/GedeonIsezerano/Devbox/main/install.sh | sh              # CLI only
#   curl -fsSL https://raw.githubusercontent.com/GedeonIsezerano/Devbox/main/install.sh | sh -s -- --all  # CLI + server

REPO="GedeonIsezerano/Devbox"

# Parse arguments
INSTALL_SERVER=false
for arg in "$@"; do
    case "$arg" in
        --all|--server) INSTALL_SERVER=true ;;
    esac
done

# Detect OS
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$OS" in
    darwin) OS="darwin" ;;
    linux) OS="linux" ;;
    *) echo "Error: Unsupported OS: $OS"; exit 1 ;;
esac

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
    x86_64|amd64) ARCH="amd64" ;;
    arm64|aarch64) ARCH="arm64" ;;
    *) echo "Error: Unsupported architecture: $ARCH"; exit 1 ;;
esac

# Get latest version
VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/')
if [ -z "$VERSION" ]; then
    echo "Error: Could not determine latest version"
    exit 1
fi

# Determine install directory
INSTALL_DIR="/usr/local/bin"
if [ ! -w "$INSTALL_DIR" ]; then
    INSTALL_DIR="${HOME}/.local/bin"
    mkdir -p "$INSTALL_DIR"
fi

TMPDIR=$(mktemp -d)
CHECKSUM_URL="https://github.com/${REPO}/releases/download/${VERSION}/checksums.txt"
curl -fsSL "$CHECKSUM_URL" -o "${TMPDIR}/checksums.txt"

# Install a single binary
install_binary() {
    BINARY="$1"
    ARCHIVE="${BINARY}-${OS}-${ARCH}.tar.gz"
    URL="https://github.com/${REPO}/releases/download/${VERSION}/${ARCHIVE}"

    echo "Downloading ${BINARY} ${VERSION} for ${OS}/${ARCH}..."
    curl -fsSL "$URL" -o "${TMPDIR}/${ARCHIVE}"

    # Verify checksum
    cd "$TMPDIR"
    EXPECTED=$(grep "${ARCHIVE}" checksums.txt | awk '{print $1}')
    if [ -z "$EXPECTED" ]; then
        echo "Error: Checksum not found for ${ARCHIVE}"
        rm -rf "$TMPDIR"
        exit 1
    fi

    if command -v sha256sum > /dev/null 2>&1; then
        ACTUAL=$(sha256sum "${ARCHIVE}" | awk '{print $1}')
    elif command -v shasum > /dev/null 2>&1; then
        ACTUAL=$(shasum -a 256 "${ARCHIVE}" | awk '{print $1}')
    else
        echo "Warning: No sha256sum or shasum found, skipping checksum verification"
        ACTUAL="$EXPECTED"
    fi

    if [ "$ACTUAL" != "$EXPECTED" ]; then
        echo "Error: Checksum mismatch for ${ARCHIVE}!"
        echo "  Expected: $EXPECTED"
        echo "  Actual:   $ACTUAL"
        rm -rf "$TMPDIR"
        exit 1
    fi

    # Extract and install
    tar -xzf "${ARCHIVE}"
    EXTRACTED="${BINARY}-${OS}-${ARCH}"
    mv "${EXTRACTED}" "${INSTALL_DIR}/${BINARY}"
    chmod +x "${INSTALL_DIR}/${BINARY}"

    echo "Installed ${BINARY} ${VERSION} to ${INSTALL_DIR}/${BINARY}"
}

# Always install the CLI
install_binary "dbx"

# Optionally install the server
if [ "$INSTALL_SERVER" = true ]; then
    install_binary "dbx-server"
fi

# Cleanup
rm -rf "$TMPDIR"

if [ "$INSTALL_DIR" = "${HOME}/.local/bin" ]; then
    echo ""
    echo "Make sure ${INSTALL_DIR} is in your PATH:"
    echo "  export PATH=\"\$HOME/.local/bin:\$PATH\""
fi
