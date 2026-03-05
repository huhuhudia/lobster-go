#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$SCRIPT_DIR"
BINARY_NAME="lobster-go"
MIN_GO_MAJOR=1
MIN_GO_MINOR=22

print_help() {
  cat <<'USAGE'
Usage: ./install.sh [--skip-tests] [--bin-dir <dir>] [--help]

Build and install lobster-go from source.

Options:
  --skip-tests       Skip `go test ./...` before build
  --bin-dir <dir>    Install directory for binary
  --help             Show this help

Environment variables:
  BIN_DIR            Same as --bin-dir
  SKIP_TESTS=1       Same as --skip-tests
USAGE
}

log() {
  printf '[install] %s\n' "$*"
}

fail() {
  printf '[install] ERROR: %s\n' "$*" >&2
  exit 1
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"
}

parse_go_version() {
  local raw version core
  raw="$(go version)"
  version="$(awk '{print $3}' <<<"$raw")"
  core="${version#go}"
  core="${core%%[^0-9.]*}"
  printf '%s' "$core"
}

check_go_version() {
  local go_version major minor
  go_version="$(parse_go_version)"
  major="${go_version%%.*}"
  minor="${go_version#*.}"
  minor="${minor%%.*}"

  if [[ -z "$major" || -z "$minor" ]]; then
    fail "failed to parse Go version: $go_version"
  fi

  if (( major < MIN_GO_MAJOR )) || { (( major == MIN_GO_MAJOR )) && (( minor < MIN_GO_MINOR )); }; then
    fail "Go ${MIN_GO_MAJOR}.${MIN_GO_MINOR}+ is required, current: $go_version"
  fi

  log "Go version OK: $go_version"
}

pick_bin_dir() {
  if [[ -n "${BIN_DIR:-}" ]]; then
    printf '%s' "$BIN_DIR"
    return
  fi

  if [[ -w "/usr/local/bin" ]]; then
    printf '%s' "/usr/local/bin"
    return
  fi

  printf '%s' "$HOME/.local/bin"
}

install_binary() {
  local build_out install_dir install_target
  install_dir="$1"
  build_out="$REPO_ROOT/$BINARY_NAME"
  install_target="$install_dir/$BINARY_NAME"

  mkdir -p "$install_dir"
  cp "$build_out" "$install_target"
  chmod +x "$install_target"

  log "Installed: $install_target"

  case ":$PATH:" in
    *":$install_dir:"*) ;;
    *)
      log "PATH does not include $install_dir"
      log "Add this line to your shell profile:"
      log "  export PATH=\"$install_dir:\$PATH\""
      ;;
  esac
}

SKIP_TESTS="${SKIP_TESTS:-0}"
BIN_DIR="${BIN_DIR:-}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --skip-tests)
      SKIP_TESTS=1
      shift
      ;;
    --bin-dir)
      [[ $# -ge 2 ]] || fail "--bin-dir requires a value"
      BIN_DIR="$2"
      shift 2
      ;;
    --help|-h)
      print_help
      exit 0
      ;;
    *)
      fail "unknown option: $1"
      ;;
  esac
done

require_cmd go
check_go_version

log "Repository: $REPO_ROOT"
cd "$REPO_ROOT"

if [[ "$SKIP_TESTS" != "1" ]]; then
  log "Running tests"
  go test ./...
else
  log "Skipping tests"
fi

log "Building $BINARY_NAME"
go build -o "$BINARY_NAME" ./cmd/lobster-go

TARGET_BIN_DIR="$(pick_bin_dir)"
log "Installing to $TARGET_BIN_DIR"
install_binary "$TARGET_BIN_DIR"

log "Done"
log "Try: $BINARY_NAME version"
