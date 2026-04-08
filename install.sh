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

info "Installing paperless-aissist $VERSION..."

# ─── create user & directories ───────────────────────────────────────────────
if ! id "$SERVICE_USER" &>/dev/null; then
  useradd -r -s /bin/false -d "$INSTALL_DIR" "$SERVICE_USER"
  success "Created user $SERVICE_USER"
fi

mkdir -p "$INSTALL_DIR/data"

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

# ─── enable & start service ──────────────────────────────────────────────────
systemctl daemon-reload
systemctl enable paperless-aissist

echo ""
echo "────────────────────────────────────────────────────"
success "paperless-aissist $VERSION installed successfully!"
echo ""
echo "  1. Edit the config:   nano $INSTALL_DIR/.env"
echo "  2. Start the service: systemctl start paperless-aissist"
echo "  3. Check status:      systemctl status paperless-aissist"
echo "  4. View logs:         journalctl -u paperless-aissist -f"
echo "────────────────────────────────────────────────────"
