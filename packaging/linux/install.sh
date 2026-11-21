#!/bin/sh
# Installs jam for the current user and adds it to the app launcher.
# It sits in the jam folder the download unpacks to; run it again after
# downloading a newer build to update.
set -eu
here=$(cd "$(dirname "$0")" && pwd)
bin="$HOME/.local/bin"
apps="$HOME/.local/share/applications"
icons="$HOME/.local/share/icons/hicolor/256x256/apps"
mkdir -p "$bin" "$apps" "$icons"
install -m 755 "$here/jam" "$bin/jam"
install -m 644 "$here/jam.png" "$icons/jam.png"
cat > "$apps/jam.desktop" <<EOF
[Desktop Entry]
Type=Application
Name=jam
Comment=Rhythm game
Exec=$bin/jam
Icon=jam
Terminal=false
Categories=Game;
EOF
update-desktop-database "$apps" 2>/dev/null || true
if ! command -v ffmpeg >/dev/null 2>&1; then
	echo "Note: ffmpeg isn't installed; songs in opus or flac need it (sudo apt install ffmpeg)."
fi
echo "Installed jam $(cat "$here/VERSION" 2>/dev/null || echo) to $bin/jam. Find it in the app launcher."
