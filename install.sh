#!/usr/bin/env bash
set -euo pipefail

# ─── paperless-aissist installer ─────────────────────────────────────────────
REPO="ulfalfa/paperless-aissist"
INSTALL_DIR="/opt/paperless-aissist"
SERVICE_USER="paperless-aissist"
SERVICE_FILE="/etc/systemd/system/paperless-aissist.service"
GITLAB_API="https://gitlab.com/api/v4/projects/${REPO/\//%2F}"

# ─── helpers ─────────────────────────────────────────────────────────────────
info()    { echo -e "\033[1;34m==>\033[0m $*"; }
success() { echo -e "\033[1;32m✓\033[0m $*"; }
warn()    { echo -e "\033[1;33m!\033[0m $*"; }
die()     { echo -e "\033[1;31m✗\033[0m $*" >&2; exit 1; }

# ─── checks ──────────────────────────────────────────────────────────────────
[[ "$(uname -s)" == "Linux" ]] || die "This installer only supports Linux."
[[ "$EUID" -eq 0 ]]            || die "Please run as root: sudo bash install.sh"

ARCH=$(uname -m)
case "$ARCH" in
  x86_64)  BINARY="paperless-aissist-linux-amd64" ;;
  aarch64) BINARY="paperless-aissist-linux-arm64" ;;
  *)       die "Unsupported architecture: $ARCH" ;;
esac

# ─── latest release ──────────────────────────────────────────────────────────
info "Fetching latest release..."
RELEASE_JSON=$(curl -fsSL "${GITLAB_API}/releases/permalink/latest")
VERSION=$(echo "$RELEASE_JSON" | grep -o '"tag_name":"[^"]*"' | head -1 | cut -d'"' -f4)
[[ -n "$VERSION" ]] || die "Could not determine latest version."

BINARY_URL=$(echo "$RELEASE_JSON" | grep -o "\"url\":\"[^\"]*${BINARY}\"" | head -1 | cut -d'"' -f4)
[[ -n "$BINARY_URL" ]] || die "No binary found for $ARCH in release $VERSION."

# ─── check current version ───────────────────────────────────────────────────
CURRENT_VERSION=""
if [[ -x "$INSTALL_DIR/paperless-aissist" ]]; then
  CURRENT_VERSION=$("$INSTALL_DIR/paperless-aissist" --version 2>/dev/null || true)
fi

if [[ "$CURRENT_VERSION" == "$VERSION" ]]; then
  success "paperless-aissist $VERSION is already up to date."
  exit 0
fi

if [[ -n "$CURRENT_VERSION" ]]; then
  info "Updating paperless-aissist $CURRENT_VERSION → $VERSION..."
else
  info "Installing paperless-aissist $VERSION..."
fi

# ─── create user & directories ───────────────────────────────────────────────
if ! id "$SERVICE_USER" &>/dev/null; then
  useradd -r -s /bin/false -d "$INSTALL_DIR" "$SERVICE_USER"
  success "Created user $SERVICE_USER"
fi

mkdir -p "$INSTALL_DIR/data"

# ─── stop service before update ──────────────────────────────────────────────
if systemctl is-active --quiet paperless-aissist 2>/dev/null; then
  info "Stopping service..."
  systemctl stop paperless-aissist
fi

# ─── download binary ─────────────────────────────────────────────────────────
info "Downloading $BINARY..."
curl -fsSL "$BINARY_URL" -o "$INSTALL_DIR/paperless-aissist"
chmod +x "$INSTALL_DIR/paperless-aissist"
success "Binary installed to $INSTALL_DIR/paperless-aissist"

# ─── download service file ───────────────────────────────────────────────────
curl -fsSL "https://gitlab.com/${REPO}/-/raw/main/paperless-aissist.service" \
  -o "$SERVICE_FILE"
success "Service file installed to $SERVICE_FILE"

# ─── create .env if not present ──────────────────────────────────────────────
if [[ ! -f "$INSTALL_DIR/.env" ]]; then
  curl -fsSL "https://gitlab.com/${REPO}/-/raw/main/.env.example" \
    -o "$INSTALL_DIR/.env"
  success "Created $INSTALL_DIR/.env from example"
  warn "Edit $INSTALL_DIR/.env before starting the service!"
else
  success ".env already exists — skipping"
fi

# ─── fix permissions ─────────────────────────────────────────────────────────
chown -R "$SERVICE_USER:$SERVICE_USER" "$INSTALL_DIR"

# ─── reload & restart service ────────────────────────────────────────────────
systemctl daemon-reload
systemctl enable paperless-aissist
systemctl restart paperless-aissist
success "Service restarted"

echo ""
echo "────────────────────────────────────────────────────"
if [[ -n "$CURRENT_VERSION" ]]; then
  success "paperless-aissist updated $CURRENT_VERSION → $VERSION"
else
  success "paperless-aissist $VERSION installed successfully!"
  echo ""
  echo "  Edit the config:   nano $INSTALL_DIR/.env"
fi
echo ""
echo "  Check status:      systemctl status paperless-aissist"
echo "  View logs:         journalctl -u paperless-aissist -f"
echo "────────────────────────────────────────────────────"
