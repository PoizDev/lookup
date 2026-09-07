#!/bin/sh
set -eu

repository="poizdev/lookup"
base_api="https://api.github.com/repos/$repository"
version="${LOOKUP_VERSION:-latest}"
install_dir="${LOOKUP_INSTALL_DIR:-${HOME:?HOME is required}/.local/bin}"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT HUP INT TERM

download() {
  if command -v curl >/dev/null 2>&1; then
    curl -sSfL "$1" -o "$2"
  elif command -v wget >/dev/null 2>&1; then
    wget -q "$1" -O "$2"
  else
    echo "lookup installer requires curl or wget" >&2
    exit 1
  fi
}

print_wordmark() {
  if [ -t 1 ] && [ "${TERM:-}" != "dumb" ] && command -v tput >/dev/null 2>&1; then
    columns="$(tput cols 2>/dev/null || printf '0')"
    case "$columns" in
      ''|*[!0-9]*) columns=0 ;;
    esac
    if [ "$columns" -ge 74 ]; then
      # LOOKUP_WORDMARK_BEGIN
      cat <<'EOF'
⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⣶⣶⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⢰⣶⡆⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⣀⣀⡀⠀⢀⣠⣤⣀⣀⠀⠀⠀
⢠⡀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⢀⣶⣶⠄⠀⠀⠀⠀⣿⣿⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⢸⣿⡇⠀⠀⠀⠀⠀⠀⣀⣀⠀⠀⠀⠀⠀⠀⣀⣀⠿⢿⣧⣼⠿⠛⠛⠻⢟⣷⣄⠀
⠘⣧⠀⠀⠀⠀⠀⣀⣤⡀⠀⠀⣼⣟⡟⠀⠀⠀⠀⠀⣿⣿⠀⠀⣠⣴⣶⣿⣷⣶⣤⡀⠀⠀⣀⣴⣶⣾⣿⣶⣤⣀⠀⢸⣿⡇⠀⠀⠀⣠⣶⡶⣿⣻⠀⠀⠀⠀⠀⠀⣿⣻⠀⢸⣯⡇⠀⠀⠀⠀⠀⢻⢿⡄
⠀⢹⡆⠀⠀⠀⢰⣿⠿⣿⣦⢰⣿⡻⠀⠀⠀⠀⠀⠀⣿⣿⢀⣾⣿⠏⠁⠀⠀⠉⢿⣿⡆⣼⣿⠟⠃⠀⠀⠉⠻⣿⣦⢸⣿⡇⠀⣤⣾⣿⠏⠀⣿⣽⠀⠀⠀⠀⠀⠀⣿⣽⠀⢸⣿⠆⠀⠀⠀⠀⠀⢸⣿⡇
⠀⠀⣿⡄⠀⢀⣿⡟⠄⠹⣿⣿⡿⠁⠀⠀⠀⠀⠀⠀⣿⣿⢸⣿⡇⠀⠀⠀⠀⠀⠈⣿⣿⣿⡏⠀⠀⠀⠀⠀⠀⣿⣿⣼⣿⣧⣾⣿⣅⠀⠀⠀⣿⢾⠀⠀⠀⠀⠀⠀⣿⣾⠀⢸⣿⣧⡀⠀⠀⠀⣠⣿⢾⠁
⠀⠀⢸⣧⠀⣼⡿⠁⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⣿⣿⢸⣿⣇⠀⠀⠀⠀⠀⢀⣿⡿⣿⣧⠀⠀⠀⠀⠀⠀⣿⣿⢻⣿⡟⠁⠻⣿⣦⡀⠀⢻⣟⣇⠀⠀⠀⠀⣰⣿⣽⡀⢺⡿⡜⠿⣷⣶⣿⡻⠝⠁⠀
⠀⠀⠀⢿⣧⣿⠃⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⣿⣿⠀⠻⣿⣦⣄⣀⣠⣤⣾⡿⠃⠹⣿⣷⣤⣀⣠⣤⣾⣿⠇⢸⣿⡇⠀⠀⠈⠻⣿⣦⠀⠙⢿⣻⣷⣿⡿⠛⠈⣿⣻⢸⣟⡇⠀⠀⠀⠀⠀⠀⠀⠀
⠀⠀⠀⠀⠉⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠛⠛⠂⠀⠈⠙⠛⠛⠛⠛⠉⠀⠀⠀⠀⠉⠛⠛⠛⠛⠉⠀⠀⠘⠛⠓⠀⠀⠀⠀⠙⠛⠛⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⣹⣯⡇⠀⠀⠀⠀⠀⠀⠀⠀
EOF
      # LOOKUP_WORDMARK_END
      return
    fi
  fi
  printf 'lookup\n'
}

print_success() {
  print_wordmark
  printf '\n✓ Lookup %s installed\n\n' "$release_version"
  printf 'Binary\n  %s/lookup\n\n' "$install_dir"
  case ":$PATH:" in
    *":$install_dir:"*)
      printf 'Get started\n  lookup init\n\n'
      printf 'Then analyze a project\n  lookup .\n'
      ;;
    *)
      printf '! %s is not in PATH\n\n' "$install_dir"
      printf 'Add it to your shell:\n  export PATH="%s:$PATH"\n\n' "$install_dir"
      printf 'Then run:\n  lookup init\n'
      ;;
  esac
}

if [ "$version" = "latest" ]; then
  metadata="$tmp_dir/release.json"
  download "$base_api/releases/latest" "$metadata"
  version="$(sed -n 's/.*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' "$metadata" | head -n 1)"
fi
if ! printf '%s\n' "$version" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$'; then
  echo "invalid LOOKUP_VERSION: $version (expected vX.Y.Z or vX.Y.Z-prerelease)" >&2
  exit 1
fi

case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  *) echo "unsupported operating system: $(uname -s)" >&2; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

release_version="${version#v}"
suffix="${os}_${arch}"
if [ "$os" = linux ]; then suffix="${suffix}_musl"; fi
archive="lookup_${release_version}_${suffix}.tar.gz"
release_url="${LOOKUP_RELEASE_BASE_URL:-https://github.com/$repository/releases/download/$version}"
download "$release_url/$archive" "$tmp_dir/$archive"
download "$release_url/checksums.txt" "$tmp_dir/checksums.txt"

expected="$(awk -v file="$archive" '$2 == file {print $1}' "$tmp_dir/checksums.txt")"
if [ -z "$expected" ]; then
  echo "checksum entry not found for $archive" >&2
  exit 1
fi
if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$tmp_dir/$archive" | awk '{print $1}')"
elif command -v shasum >/dev/null 2>&1; then
  actual="$(shasum -a 256 "$tmp_dir/$archive" | awk '{print $1}')"
else
  echo "lookup installer requires sha256sum or shasum" >&2
  exit 1
fi
if [ "$actual" != "$expected" ]; then
  echo "checksum verification failed for $archive" >&2
  exit 1
fi

tar -xzf "$tmp_dir/$archive" -C "$tmp_dir"
mkdir -p "$install_dir"
install_dir="$(cd "$install_dir" && pwd -P)"
chmod 755 "$tmp_dir/lookup"
mv "$tmp_dir/lookup" "$install_dir/lookup"
case "$os" in
  darwin) receipt_dir="$HOME/Library/Application Support/lookup" ;;
  linux)
    case "${XDG_CONFIG_HOME:-}" in
      /*) receipt_dir="$XDG_CONFIG_HOME/lookup" ;;
      *) receipt_dir="$HOME/.config/lookup" ;;
    esac
    ;;
esac
mkdir -p "$receipt_dir"
chmod 700 "$receipt_dir" 2>/dev/null || true
escaped_install_path="$(printf '%s' "$install_dir/lookup" | sed 's/\\/\\\\/g; s/"/\\"/g')"
umask 077
printf '{\n  "method": "official-installer",\n  "install_path": "%s",\n  "repository": "poizdev/lookup"\n}\n' "$escaped_install_path" > "$tmp_dir/install.json"
mv "$tmp_dir/install.json" "$receipt_dir/install.json"
print_success
