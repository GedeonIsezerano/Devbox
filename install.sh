#!/bin/sh
set -e

# Devbox CLI installer
# Usage: curl -fsSL https://raw.githubusercontent.com/user/devbox/main/install.sh | sh

REPO="user/devbox"
BINARY="dbx"

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

# Download
ARCHIVE="${BINARY}_${OS}_${ARCH}.tar.gz"
URL="https://github.com/${REPO}/releases/download/${VERSION}/${ARCHIVE}"
CHECKSUM_URL="https://github.com/${REPO}/releases/download/${VERSION}/checksums.txt"

echo "Downloading ${BINARY} ${VERSION} for ${OS}/${ARCH}..."
TMPDIR=$(mktemp -d)
curl -fsSL "$URL" -o "${TMPDIR}/${ARCHIVE}"
curl -fsSL "$CHECKSUM_URL" -o "${TMPDIR}/checksums.txt"

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
    echo "Error: Checksum mismatch!"
    echo "  Expected: $EXPECTED"
    echo "  Actual:   $ACTUAL"
    rm -rf "$TMPDIR"
    exit 1
fi

# Extract
tar -xzf "${ARCHIVE}"

# Install
INSTALL_DIR="/usr/local/bin"
if [ ! -w "$INSTALL_DIR" ]; then
    INSTALL_DIR="${HOME}/.local/bin"
    mkdir -p "$INSTALL_DIR"
fi

mv "${BINARY}" "${INSTALL_DIR}/${BINARY}"
chmod +x "${INSTALL_DIR}/${BINARY}"

# Cleanup
rm -rf "$TMPDIR"

echo "Installed ${BINARY} ${VERSION} to ${INSTALL_DIR}/${BINARY}"
if [ "$INSTALL_DIR" = "${HOME}/.local/bin" ]; then
    echo "Make sure ${INSTALL_DIR} is in your PATH"
fi
