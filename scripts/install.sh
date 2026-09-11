#!/usr/bin/env sh
# Installs mef and mef-mcp on macOS/Linux.
#
# From a checkout (works today):   ./scripts/install.sh
# From a release (once published): ./scripts/install.sh --release [version]
set -eu

if [ "${1:-}" != "--release" ]; then
  command -v go >/dev/null 2>&1 || { echo "Go >= 1.26 is required: https://go.dev/dl/" >&2; exit 1; }
  cd "$(dirname "$0")/.."
  go install ./cmd/...
  bin="$(go env GOPATH)/bin"
else
  version="${2:-latest}"
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  case "$(uname -m)" in x86_64|amd64) arch=amd64 ;; arm64|aarch64) arch=arm64 ;; *) echo "unsupported arch" >&2; exit 1 ;; esac
  if [ "$version" = latest ]; then api="https://api.github.com/repos/JOSETRA44/mef-mcp/releases/latest"; else api="https://api.github.com/repos/JOSETRA44/mef-mcp/releases/tags/v$version"; fi
  url="$(curl -fsSL "$api" | grep -o "https://[^\"]*_${os}_${arch}.tar.gz" | head -n1)"
  [ -n "$url" ] || { echo "no ${os}/${arch} asset found" >&2; exit 1; }
  bin="${HOME}/.local/bin"
  mkdir -p "$bin"
  curl -fsSL "$url" | tar -xz -C "$bin" mef mef-mcp
fi

case ":$PATH:" in *":$bin:"*) ;; *) echo "Add $bin to your PATH." ;; esac
"$bin/mef" --version
echo "Register with Claude Code:  claude mcp add mef -- mef-mcp"
