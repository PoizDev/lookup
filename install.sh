#!/bin/sh
set -eu

repository="poizdev/lookup"
base_api="https://api.github.com/repos/$repository"
version="${LOOKUP_VERSION:-latest}"
install_dir="${LOOKUP_INSTALL_DIR:-${HOME:?HOME is required}/.local/bin}"
tmp_dir="$(mktemp -d)"
spinner_pid=
spinner_message=
path_added=false
path_configured=false
path_config_file=

spinner_start() {
  spinner_message=$1
  if [ ! -t 2 ] || [ "${TERM:-}" = "dumb" ]; then
    return
  fi
  (
    while :; do
      for frame in ⠋ ⠙ ⠹ ⠸ ⠼ ⠴ ⠦ ⠧ ⠇ ⠏; do
        printf '\r%s %s...' "$frame" "$spinner_message" >&2
        sleep 0.1
      done
    done
  ) &
  spinner_pid=$!
}

spinner_stop() {
  status=$1
  completion=${2:-}
  if [ -n "$spinner_pid" ]; then
    kill "$spinner_pid" 2>/dev/null || true
    wait "$spinner_pid" 2>/dev/null || true
    spinner_pid=
    printf '\r                                                                                \r' >&2
  fi
  if [ "$status" -eq 0 ] && [ -n "$completion" ]; then
    printf '✓ %s\n' "$completion" >&2
  fi
}

cleanup() {
  status=$?
  spinner_stop "$status" ""
  rm -rf "$tmp_dir"
  exit "$status"
}

on_signal() {
  trap - EXIT
  spinner_stop "$1" ""
  rm -rf "$tmp_dir"
  exit "$1"
}

trap cleanup EXIT
trap 'on_signal 129' HUP
trap 'on_signal 130' INT
trap 'on_signal 143' TERM

run_phase() {
  message=$1
  completion=$2
  shift 2
  spinner_start "$message"
  set +e
  "$@"
  status=$?
  set -e
  spinner_stop "$status" "$completion"
  return "$status"
}

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

path_contains_dir() {
  old_ifs=$IFS
  IFS=:
  set -f
  set -- ${PATH:-}
  set +f
  IFS=$old_ifs
  for path_entry do
    if [ "${path_entry%/}" = "${install_dir%/}" ]; then return 0; fi
  done
  return 1
}

shell_name() {
  case "${1##*/}" in
    zsh|bash|fish) printf '%s\n' "${1##*/}" ;;
    *) return 1 ;;
  esac
}

detect_shell() {
  configured_shell=
  environment_shell=
  account_shell=
  environment_shell_path=${SHELL:-}

  if [ -n "${USER:-}" ] && command -v getent >/dev/null 2>&1; then
    account_shell="$(getent passwd "$USER" 2>/dev/null | awk -F: 'NR == 1 {print $7}')"
  elif [ -n "${USER:-}" ] && command -v dscl >/dev/null 2>&1; then
    account_shell="$(dscl . -read "/Users/$USER" UserShell 2>/dev/null | awk 'NR == 1 {print $2}')"
  fi

  if [ -n "$account_shell" ]; then
    configured_shell="$(shell_name "$account_shell" 2>/dev/null || true)"
  fi
  if [ -n "$environment_shell_path" ]; then
    environment_shell="$(shell_name "$environment_shell_path" 2>/dev/null || true)"
  fi

  if [ -n "$account_shell" ] && [ "${account_shell##*/}" != "${environment_shell_path##*/}" ]; then
    return 1
  fi
  if [ -n "$configured_shell" ]; then
    printf '%s\n' "$configured_shell"
  elif [ -z "$account_shell" ] && [ -n "$environment_shell" ]; then
    printf '%s\n' "$environment_shell"
  else
    return 1
  fi
}

display_path() {
  case "$1" in
    "$HOME") printf '~\n' ;;
    "$HOME"/*) printf '~/%s\n' "${1#"$HOME"/}" ;;
    *) printf '%s\n' "$1" ;;
  esac
}

double_quote_path() {
  printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g; s/\$/\\$/g; s/`/\\`/g'
}

persist_path() {
  path_contains_dir && return 0

  detected_shell="$(detect_shell 2>/dev/null || true)"
  case "$detected_shell" in
    zsh) path_config_file="$HOME/.zshrc" ;;
    bash)
      if [ -f "$HOME/.bashrc" ]; then
        path_config_file="$HOME/.bashrc"
      elif [ -f "$HOME/.bash_profile" ]; then
        path_config_file="$HOME/.bash_profile"
      else
        path_config_file="$HOME/.bashrc"
      fi
      ;;
    fish) path_config_file="$HOME/.config/fish/config.fish" ;;
    *) path_config_file=; return 0 ;;
  esac

  if [ "$install_dir" = "$HOME/.local/bin" ]; then
    path_value='$HOME/.local/bin'
  elif [ "${install_dir#"$HOME"/}" != "$install_dir" ]; then
    path_value="\$HOME/$(double_quote_path "${install_dir#"$HOME"/}")"
  else
    path_value="$(double_quote_path "$install_dir")"
  fi
  if [ "$detected_shell" = fish ]; then
    path_line="fish_add_path \"$path_value\""
  else
    path_line="export PATH=\"$path_value:\$PATH\""
  fi

  if [ -f "$path_config_file" ] && grep -Fqx "$path_line" "$path_config_file"; then
    path_configured=true
    return 0
  fi
  mkdir -p "${path_config_file%/*}"
  if [ -s "$path_config_file" ]; then
    last_char="$(tail -c 1 "$path_config_file"; printf x)"
    if [ "$last_char" != x ]; then printf '\n' >> "$path_config_file"; fi
  fi
  printf '%s\n' "$path_line" >> "$path_config_file"
  path_added=true
  path_configured=true
}

print_success() {
  print_wordmark
  printf '\n✓ Lookup %s installed\n\n' "$release_version"
  printf 'Binary\n  %s/lookup\n\n' "$install_dir"
  if path_contains_dir; then
      printf 'Get started\n  lookup init\n\n'
      printf 'Then analyze a project\n  lookup .\n'
  elif [ "$path_configured" = true ]; then
      if [ "$path_added" = true ]; then
        printf '✓ Added %s to PATH in %s\n\n' "$(display_path "$install_dir")" "$(display_path "$path_config_file")"
      else
        printf '✓ %s is configured in %s\n\n' "$(display_path "$install_dir")" "$(display_path "$path_config_file")"
      fi
      printf 'Restart your terminal or run:\n  source %s\n\n' "$(display_path "$path_config_file")"
      printf 'Then:\n  lookup init\n'
  else
      printf '! %s is not in PATH\n\n' "$install_dir"
      printf 'Add it to your shell:\n  export PATH="%s:$PATH"\n\n' "$install_dir"
      printf 'Then run:\n  lookup init\n'
  fi
}

if [ "$version" = "latest" ]; then
  metadata="$tmp_dir/release.json"
  run_phase "Downloading release metadata" "Downloaded release metadata" download "$base_api/releases/latest" "$metadata"
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
run_phase "Downloading lookup $version" "Downloaded lookup $version" download "$release_url/$archive" "$tmp_dir/$archive"
run_phase "Downloading checksums" "Downloaded checksums" download "$release_url/checksums.txt" "$tmp_dir/checksums.txt"

expected="$(awk -v file="$archive" '$2 == file {print $1}' "$tmp_dir/checksums.txt")"
if [ -z "$expected" ]; then
  echo "checksum entry not found for $archive" >&2
  exit 1
fi
verify_checksum() {
  if command -v sha256sum >/dev/null 2>&1; then
    actual="$(sha256sum "$tmp_dir/$archive" | awk '{print $1}')"
  elif command -v shasum >/dev/null 2>&1; then
    actual="$(shasum -a 256 "$tmp_dir/$archive" | awk '{print $1}')"
  else
    echo "lookup installer requires sha256sum or shasum" >&2
    return 1
  fi
  if [ "$actual" != "$expected" ]; then
    echo "checksum verification failed for $archive" >&2
    return 1
  fi
}
run_phase "Verifying checksum" "Checksum verified" verify_checksum

run_phase "Extracting lookup" "Extracted lookup" tar -xzf "$tmp_dir/$archive" -C "$tmp_dir"
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
persist_path
print_success
