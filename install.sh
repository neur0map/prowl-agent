#!/bin/sh
# Install the latest prowl-agent build. Usage:
#   curl -fsSL https://raw.githubusercontent.com/neur0map/prowl-agent/main/install.sh | sh
set -e

REPO="neur0map/prowl-agent"
BASE="https://github.com/$REPO/releases/download/nightly"
BIN="prowl-agent-linux-amd64"
DEST="${PROWL_INSTALL_DIR:-$HOME/.local/bin}"

os="$(uname -s)"
arch="$(uname -m)"
if [ "$os" != "Linux" ]; then
  echo "prowl-agent supports Linux only (found: $os)." >&2
  exit 1
fi
if [ "$arch" != "x86_64" ] && [ "$arch" != "amd64" ]; then
  echo "prowl-agent ships an x86_64 build only (found: $arch). Build from source instead." >&2
  exit 1
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "Downloading prowl-agent ..."
curl -fsSL -o "$tmp/$BIN" "$BASE/$BIN"
curl -fsSL -o "$tmp/$BIN.sha256" "$BASE/$BIN.sha256"

echo "Verifying checksum ..."
want="$(awk '{print $1}' "$tmp/$BIN.sha256")"
if command -v sha256sum >/dev/null 2>&1; then
  got="$(sha256sum "$tmp/$BIN" | awk '{print $1}')"
else
  got="$(shasum -a 256 "$tmp/$BIN" | awk '{print $1}')"
fi
if [ "$want" != "$got" ]; then
  echo "Checksum mismatch; aborting." >&2
  exit 1
fi

mkdir -p "$DEST"
install -m 0755 "$tmp/$BIN" "$DEST/prowl-agent"

echo ""
echo "  prowl-agent installed to $DEST/prowl-agent"
echo "  next:  cd <your project> && prowl-agent init"
echo ""

case ":$PATH:" in
  *":$DEST:"*) ;;
  *)
    echo "  note: $DEST is not on your PATH yet. Add it, e.g.:"
    echo "        export PATH=\"$DEST:\$PATH\""
    echo ""
    ;;
esac
