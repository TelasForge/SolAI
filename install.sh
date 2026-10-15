#!/usr/bin/env bash
set -euo pipefail

REPO="melodyogonna/solai"
BIN_DIR="${HOME}/.local/bin"

# Resolve architecture
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64)  ARCH="amd64" ;;
  aarch64) ARCH="arm64" ;;
  *)
    echo "error: unsupported architecture: $ARCH (supported: x86_64, aarch64)" >&2
    exit 1
    ;;
esac

install_bin() {
  local name="$1"
  local asset="${name}-linux-${ARCH}"
  local url="https://github.com/${REPO}/releases/latest/download/${asset}"
  echo "Downloading ${asset} from ${REPO}..."
  curl -fsSL "$url" -o "${BIN_DIR}/${name}"
  chmod +x "${BIN_DIR}/${name}"
}

mkdir -p "$BIN_DIR"
install_bin solai
install_bin solaid

echo "Installed solai and solaid to ${BIN_DIR}"

# Advise on PATH if needed
if ! echo "$PATH" | tr ':' '\n' | grep -qx "$BIN_DIR"; then
  echo ""
  echo "Add ${BIN_DIR} to your PATH by running:"
  echo ""
  echo "    echo 'export PATH=\"\$HOME/.local/bin:\$PATH\"' >> ~/.bashrc && source ~/.bashrc"
  echo ""
  echo "Or for zsh:"
  echo ""
  echo "    echo 'export PATH=\"\$HOME/.local/bin:\$PATH\"' >> ~/.zshrc && source ~/.zshrc"
fi
