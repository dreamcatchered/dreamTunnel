#!/usr/bin/env sh
# dreamTunnel installer — downloads the dt binary for this platform.
#
#   curl -fsSL https://tunnel.swag.best/install.sh | sh
#
# The access token is already compiled into the binary, so there is nothing to
# configure afterwards: `dt http 8080` works immediately.
set -eu

DOMAIN="${DREAMTUNNEL_DOMAIN:-tunnel.swag.best}"
TUNNEL_DOMAIN="${DREAMTUNNEL_PUBLIC_DOMAIN:-maiuni.online}"
BASE_URL="https://${DOMAIN}"

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "$OS" in
    linux) ;;
    darwin|macos) OS="darwin" ;;
    mingw*|msys*|cygwin*) OS="windows" ;;
    *) echo "[-] unsupported OS: $OS"; exit 1 ;;
esac

case "$ARCH" in
    x86_64|amd64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    i386|i686) ARCH="386" ;;
    *) echo "[-] unsupported architecture: $ARCH"; exit 1 ;;
esac

if [ "$OS" = "windows" ]; then
    BINARY="dt-windows-${ARCH}.exe"
    EXE_NAME="dt.exe"
    DEFAULT_DIR="${LOCALAPPDATA:-$HOME}/dreamtunnel"
else
    BINARY="dt-${OS}-${ARCH}"
    EXE_NAME="dt"
    DEFAULT_DIR="${XDG_BIN_HOME:-$HOME/.local/bin}"
fi

INSTALL_DIR="${DREAMTUNNEL_INSTALL_DIR:-$DEFAULT_DIR}"
EXE_PATH="${INSTALL_DIR}/${EXE_NAME}"
URL="${BASE_URL}/dl/${BINARY}"

echo ""
echo "  [+] dreamTunnel installer"
echo "  [+] platform:    ${OS}/${ARCH}"
echo "  [+] install dir: ${INSTALL_DIR}"
echo "  [+] downloading: ${URL}"
echo ""

mkdir -p "$INSTALL_DIR" 2>/dev/null || true

if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$URL" -o "$EXE_PATH"
elif command -v wget >/dev/null 2>&1; then
    wget -q "$URL" -O "$EXE_PATH"
else
    echo "[-] neither curl nor wget is available"
    exit 1
fi
chmod +x "$EXE_PATH"

# Best-effort integrity check against the published SHA256SUMS file.
SUM_TOOL=""
if command -v sha256sum >/dev/null 2>&1; then
    SUM_TOOL="sha256sum"
elif command -v shasum >/dev/null 2>&1; then
    SUM_TOOL="shasum -a 256"
fi
if [ -n "$SUM_TOOL" ]; then
    WANT="$( (command -v curl >/dev/null 2>&1 \
        && curl -fsSL "${BASE_URL}/dl/SHA256SUMS" \
        || wget -qO- "${BASE_URL}/dl/SHA256SUMS") 2>/dev/null \
        | awk -v b="$BINARY" '$2 == b { print $1 }' || true )"
    if [ -n "$WANT" ]; then
        GOT="$($SUM_TOOL "$EXE_PATH" | awk '{ print $1 }')"
        if [ "$WANT" != "$GOT" ]; then
            echo "[-] checksum mismatch for ${BINARY} (expected ${WANT}, got ${GOT})"
            rm -f "$EXE_PATH"
            exit 1
        fi
        echo "  [+] checksum verified"
    fi
fi

case ":$PATH:" in
    *":$INSTALL_DIR:"*)
        ;;
    *)
        for rc in "$HOME/.bashrc" "$HOME/.zshrc" "$HOME/.profile"; do
            [ -f "$rc" ] || continue
            grep -q "# dreamTunnel" "$rc" 2>/dev/null && continue
            printf '\n# dreamTunnel\nexport PATH="%s:$PATH"\n' "$INSTALL_DIR" >> "$rc"
        done
        echo "  [+] added ${INSTALL_DIR} to PATH — open a new terminal"
        ;;
esac

echo ""
echo "  [+] installed: ${EXE_PATH}"
echo ""
echo "  ready to use, the access token is already built in:"
echo "      dt http 8080              expose a local HTTP server"
echo "      dt http 8080 my-site      -> https://my-site.${TUNNEL_DOMAIN}"
echo "      dt tcp 25565              raw TCP tunnel"
echo "      dt http 8080 --json       machine-readable events"
echo ""
echo "  site: https://${DOMAIN}/"
echo ""
