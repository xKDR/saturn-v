#!/bin/sh
# Install saturn-v:
#
#   curl -fsSL https://raw.githubusercontent.com/xKDR/saturn-v/main/install.sh | sh
#
# Installs the tools saturn uses (AWS CLI, rclone, the Session Manager plugin), puts
# saturn in ~/.saturn-v and on your PATH, and sets up your shell. Safe to run again:
# it skips what's already there. Mac: through Homebrew. Linux: into ~/.local, no sudo.
#
# SATURN_SKIP_TOOLS=1 updates saturn only (what `saturn update` does).
set -eu

REPO=xKDR/saturn-v
BRANCH=${SATURN_BRANCH:-main}
DEST=${SATURN_HOME:-$HOME/.saturn-v}
BIN=$HOME/.local/bin

say() { printf '\033[1m%s\033[0m\n' "$*"; }
die() { printf 'saturn install: %s\n' "$*" >&2; exit 1; }
have() { command -v "$1" >/dev/null 2>&1; }
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

have curl || die "curl is needed"
os=$(uname -s)
arch=$(uname -m)
mkdir -p "$BIN"
export PATH="$BIN:$PATH"

# --- The tools ------------------------------------------------------------------------

install_tools_mac() {
  if ! have brew; then
    die "Homebrew is needed first. Install it (https://brew.sh):
  /bin/bash -c \"\$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)\"
then run this installer again."
  fi
  for f in awscli rclone; do
    brew list --formula "$f" >/dev/null 2>&1 || { say "Installing $f"; brew install "$f"; }
  done
  brew list --cask session-manager-plugin >/dev/null 2>&1 || have session-manager-plugin ||
    { say "Installing the Session Manager plugin"; brew install --cask session-manager-plugin; }
}

install_tools_linux() {
  case $arch in
  x86_64 | amd64) aws_arch=x86_64 rclone_arch=amd64 smp_arch=64bit ;;
  aarch64 | arm64) aws_arch=aarch64 rclone_arch=arm64 smp_arch=arm64 ;;
  *) die "unsupported processor: $arch" ;;
  esac
  have unzip || die "unzip is needed (sudo apt install unzip)"
  have python3 || die "python3 is needed (sudo apt install python3)"
  have perl || die "perl is needed (sudo apt install perl)"

  if ! aws --version 2>/dev/null | grep -q '^aws-cli/2'; then
    say "Installing the AWS CLI into ~/.local"
    curl -fsSL "https://awscli.amazonaws.com/awscli-exe-linux-$aws_arch.zip" -o "$tmp/aws.zip"
    unzip -q "$tmp/aws.zip" -d "$tmp"
    "$tmp/aws/install" --install-dir "$HOME/.local/aws-cli" --bin-dir "$BIN" --update >/dev/null
  fi
  if ! have rclone; then
    say "Installing rclone into ~/.local/bin"
    curl -fsSL "https://downloads.rclone.org/rclone-current-linux-$rclone_arch.zip" -o "$tmp/rclone.zip"
    unzip -q "$tmp/rclone.zip" -d "$tmp"
    cp "$tmp"/rclone-*-linux-"$rclone_arch"/rclone "$BIN/rclone" && chmod 755 "$BIN/rclone"
  fi
  if ! have session-manager-plugin; then
    say "Installing the Session Manager plugin into ~/.local/bin"
    if have dpkg-deb; then
      curl -fsSL "https://s3.amazonaws.com/session-manager-downloads/plugin/latest/ubuntu_$smp_arch/session-manager-plugin.deb" -o "$tmp/smp.deb"
      dpkg-deb -x "$tmp/smp.deb" "$tmp/smp"
    elif have rpm2cpio && have cpio; then
      curl -fsSL "https://s3.amazonaws.com/session-manager-downloads/plugin/latest/linux_$smp_arch/session-manager-plugin.rpm" -o "$tmp/smp.rpm"
      mkdir -p "$tmp/smp" && (cd "$tmp/smp" && rpm2cpio "$tmp/smp.rpm" | cpio -idm --quiet)
    else
      die "can't unpack the Session Manager plugin: needs dpkg-deb (Debian, Ubuntu) or rpm2cpio (Fedora)"
    fi
    cp "$tmp/smp/usr/local/sessionmanagerplugin/bin/session-manager-plugin" "$BIN/" && chmod 755 "$BIN/session-manager-plugin"
  fi
  if ! have fusermount3 && ! have fusermount; then
    say "Note: the workspace mounts need FUSE. Ask your admin, or: sudo apt install fuse3"
  fi
}

if [ "${SATURN_SKIP_TOOLS:-}" != 1 ]; then
  case $os in
  Darwin) install_tools_mac ;;
  Linux) install_tools_linux ;;
  *) die "saturn runs on macOS and Linux, not $os" ;;
  esac
fi

# --- saturn itself ----------------------------------------------------------------------

if [ -d "$DEST/.git" ]; then
  say "Updating saturn (git) in $DEST"
  git -C "$DEST" pull --ff-only --quiet
else
  say "Installing saturn into $DEST"
  curl -fsSL "https://github.com/$REPO/archive/refs/heads/$BRANCH.tar.gz" -o "$tmp/saturn.tar.gz"
  mkdir -p "$tmp/saturn" && tar -xzf "$tmp/saturn.tar.gz" -C "$tmp/saturn" --strip-components 1
  [ -x "$tmp/saturn/bin/saturn" ] || die "the download doesn't look like saturn-v"
  rm -rf "$DEST.new" && mv "$tmp/saturn" "$DEST.new"
  rm -rf "$DEST" && mv "$DEST.new" "$DEST"
fi
ln -sf "$DEST/bin/saturn" "$BIN/saturn"

# --- Shell setup -------------------------------------------------------------------------
# ~/.local/bin on PATH, and the job environment loaded while connected.

case ${SHELL:-} in
*/zsh) rc=$HOME/.zshrc ;;
*/bash) if [ "$os" = Darwin ]; then rc=$HOME/.bash_profile; else rc=$HOME/.bashrc; fi ;;
*) rc=$HOME/.profile ;;
esac
touch "$rc"
if ! grep -q '# saturn-v' "$rc"; then
  cat >>"$rc" <<'EOF'

# saturn-v
case ":$PATH:" in *":$HOME/.local/bin:"*) ;; *) export PATH="$HOME/.local/bin:$PATH" ;; esac
[ -f ~/.config/saturn-v/env ] && set -a && . ~/.config/saturn-v/env && set +a
EOF
  say "Added saturn to $rc"
fi

say "Done. Open a new terminal, then:"
echo "  saturn login      # sign in (on a server: saturn login --use-device-code)"
echo "  saturn connect"
