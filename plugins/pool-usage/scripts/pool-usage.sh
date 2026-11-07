#!/usr/bin/env bash
# Shows the remaining 5-hour / 7-day usage of the account pool behind a
# sub2api gateway, using the same API key Claude Code already sends.
#
#   pool-usage.sh line    one line, for the status line (cached)
#   pool-usage.sh wrap    status line that runs the previous statusLine command
#                         (saved in <config>/pool-usage-statusline.prev) and adds the pool:
#                         - if <config>/pool-usage/ exists, it refreshes the claude-hud
#                           usage snapshot there and claude-hud draws the bars itself;
#                         - otherwise it prints our text line under the previous output.
#   pool-usage.sh hook    SessionStart hook output ({"systemMessage": ...})
#   pool-usage.sh full    multi-line breakdown, for /pool-usage
#
# Config comes from the environment Claude Code passes on (ANTHROPIC_BASE_URL,
# ANTHROPIC_AUTH_TOKEN or ANTHROPIC_API_KEY, ANTHROPIC_CUSTOM_HEADERS), falling
# back to the "env" block of <config>/settings.json.
set -u

mode="${1:-line}"
cache_ttl="${POOL_USAGE_CACHE_SECONDS:-30}"
config_dir="${CLAUDE_CONFIG_DIR:-$HOME/.claude}"

settings_env() {
  local file="$config_dir/settings.json" key="$1"
  [ -f "$file" ] || return 0
  if command -v jq >/dev/null 2>&1; then
    jq -r --arg k "$key" '.env[$k] // empty' "$file" 2>/dev/null
  elif command -v python3 >/dev/null 2>&1; then
    python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("env",{}).get(sys.argv[2],""))' "$file" "$key" 2>/dev/null
  elif command -v node >/dev/null 2>&1; then
    node -e 'const e=(JSON.parse(require("fs").readFileSync(process.argv[1],"utf8")).env||{});process.stdout.write(e[process.argv[2]]||"")' "$file" "$key" 2>/dev/null
  fi
}

base="${ANTHROPIC_BASE_URL:-$(settings_env ANTHROPIC_BASE_URL)}"
token="${ANTHROPIC_AUTH_TOKEN:-${ANTHROPIC_API_KEY:-}}"
[ -n "$token" ] || token="$(settings_env ANTHROPIC_AUTH_TOKEN)"
[ -n "$token" ] || token="$(settings_env ANTHROPIC_API_KEY)"
custom="${ANTHROPIC_CUSTOM_HEADERS:-$(settings_env ANTHROPIC_CUSTOM_HEADERS)}"

file_age() {
  local mtime
  mtime="$(stat -c %Y "$1" 2>/dev/null || stat -f %m "$1" 2>/dev/null || echo 0)"
  echo $(( $(date +%s) - mtime ))
}

# GET /v1/pool-usage?format=$1; prints the body on success, or the error and returns 1.
fetch() {
  local args=(-sS --max-time 4 -H "Authorization: Bearer $token" -w '\n%{http_code}')
  local line name value out code body err=""
  [ -n "$base" ] || { echo "ANTHROPIC_BASE_URL not set"; return 1; }
  [ -n "$token" ] || { echo "no API key (ANTHROPIC_AUTH_TOKEN)"; return 1; }
  command -v curl >/dev/null 2>&1 || { echo "curl not found"; return 1; }
  while IFS= read -r line; do
    line="${line%$'\r'}"
    [ -n "$line" ] || continue
    name="${line%%:*}"; value="${line#*:}"
    [ "$name" != "$line" ] || continue
    args+=(-H "${name}:${value}")
  done <<<"$custom"
  out="$(curl "${args[@]}" "${base%/}/v1/pool-usage?format=$1" 2>/dev/null)"
  code="${out##*$'\n'}"
  body="${out%$'\n'*}"
  body="${body%$'\n'}"
  case "$code" in
    200) printf '%s' "$body"; return 0 ;;
    401) err="API key rejected" ;;
    404) err="gateway has no /v1/pool-usage endpoint" ;;
    000|"") err="gateway unreachable" ;;
    *)
      # Cloudflare Access login pages and other HTML mean the request never reached the gateway.
      case "$body" in *"<html"*|*"<!DOCTYPE"*) err="blocked before gateway (HTTP $code)" ;; *) err="HTTP $code" ;; esac ;;
  esac
  printf '%s' "$err"
  return 1
}

cache_id() {
  printf '%s|%s' "$base" "$token" | { sha256sum 2>/dev/null || shasum -a 256 2>/dev/null || cksum; } | cut -c1-16
}

# Refresh the claude-hud snapshot at most every $cache_ttl seconds. Written via a
# temp file + rename so claude-hud never reads a half-written file.
refresh_hud_snapshot() {
  local target="$1" body
  if [ -f "$target" ] && [ "$(file_age "$target")" -lt "$cache_ttl" ]; then
    return 0
  fi
  body="$(fetch hud)" || return 0
  printf '%s\n' "$body" >"$target.tmp.$$" 2>/dev/null && mv -f "$target.tmp.$$" "$target" 2>/dev/null
  rm -f "$target.tmp.$$" 2>/dev/null
  return 0
}

if [ "$mode" = "wrap" ]; then
  input=""
  [ -t 0 ] || input="$(cat)"
  hud_dir="$config_dir/pool-usage"
  [ -d "$hud_dir" ] && refresh_hud_snapshot "$hud_dir/hud-usage.json"
  prev_file="$config_dir/pool-usage-statusline.prev"
  if [ -s "$prev_file" ]; then
    printf '%s' "$input" | bash -c "$(cat "$prev_file")"
  fi
  # claude-hud draws the bars from the snapshot; no separate text line needed.
  [ -d "$hud_dir" ] && exit 0
  mode="line"
elif [ "$mode" = "line" ] && [ ! -t 0 ]; then
  # Statusline gets session JSON on stdin; it isn't needed here.
  cat >/dev/null
fi

text=""
cache_file=""
if [ "$mode" != "full" ]; then
  cache_file="${XDG_CACHE_HOME:-$HOME/.cache}/claude-pool-usage/$(cache_id).txt"
  if [ -f "$cache_file" ] && [ "$(file_age "$cache_file")" -lt "$cache_ttl" ]; then
    text="$(cat "$cache_file")"
  fi
fi

if [ -z "$text" ]; then
  fmt="text"; [ "$mode" = "full" ] && fmt="full"
  if ! text="$(fetch "$fmt")"; then
    err="$text"
    case "$mode" in
      line) echo "pool: $err"; exit 0 ;;
      hook) exit 0 ;;
      *) echo "pool-usage: $err" >&2; exit 1 ;;
    esac
  fi
  if [ -n "$cache_file" ]; then
    mkdir -p "$(dirname "$cache_file")" 2>/dev/null && printf '%s' "$text" >"$cache_file" 2>/dev/null
  fi
fi

case "$mode" in
  hook)
    esc="$(printf '%s' "$text" | sed 's/\\/\\\\/g; s/"/\\"/g')"
    printf '{"systemMessage":"Usage pool: %s"}\n' "$esc"
    ;;
  *) printf '%s\n' "$text" ;;
esac
