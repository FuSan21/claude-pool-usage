#!/usr/bin/env bash
# Shows the remaining 5-hour / 7-day usage of the account pool behind a
# sub2api gateway, using the same API key Claude Code already sends.
#
#   pool-usage.sh line    one line, for the status line (cached)
#   pool-usage.sh wrap    status line that first runs the previous statusLine command
#                         (saved in ~/.claude/pool-usage-statusline.prev), then adds our line
#   pool-usage.sh hook    SessionStart hook output ({"systemMessage": ...})
#   pool-usage.sh full    multi-line breakdown, for /pool-usage
#
# Config comes from the environment Claude Code passes on (ANTHROPIC_BASE_URL,
# ANTHROPIC_AUTH_TOKEN or ANTHROPIC_API_KEY, ANTHROPIC_CUSTOM_HEADERS), falling
# back to the "env" block of ~/.claude/settings.json.
set -u

mode="${1:-line}"
cache_ttl="${POOL_USAGE_CACHE_SECONDS:-30}"

settings_env() {
  local file="${CLAUDE_CONFIG_DIR:-$HOME/.claude}/settings.json" key="$1"
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

# Chain mode: hand the session JSON to the status line that was there before
# (e.g. claude-hud), print its output, then continue as a normal line below it.
if [ "$mode" = "wrap" ]; then
  input=""
  [ -t 0 ] || input="$(cat)"
  prev_file="${CLAUDE_CONFIG_DIR:-$HOME/.claude}/pool-usage-statusline.prev"
  if [ -s "$prev_file" ]; then
    printf '%s' "$input" | bash -c "$(cat "$prev_file")"
  fi
  mode="line"
elif [ "$mode" = "line" ] && [ ! -t 0 ]; then
  # Statusline gets session JSON on stdin; it isn't needed here.
  cat >/dev/null
fi

fail() {
  case "$mode" in
    line) echo "pool: $1" ;;
    hook) exit 0 ;;
    *) echo "pool-usage: $1" >&2; exit 1 ;;
  esac
  exit 0
}

[ -n "$base" ] || fail "ANTHROPIC_BASE_URL not set"
[ -n "$token" ] || fail "no API key (ANTHROPIC_AUTH_TOKEN)"
command -v curl >/dev/null 2>&1 || fail "curl not found"

url="${base%/}/v1/pool-usage?format=text"
[ "$mode" = "full" ] && url="${base%/}/v1/pool-usage?format=full"

fetch() {
  local args=(-sS --max-time 4 -H "Authorization: Bearer $token" -w '\n%{http_code}')
  local line name value
  while IFS= read -r line; do
    line="${line%$'\r'}"
    [ -n "$line" ] || continue
    name="${line%%:*}"; value="${line#*:}"
    [ "$name" != "$line" ] || continue
    args+=(-H "${name}:${value}")
  done <<<"$custom"
  curl "${args[@]}" "$url" 2>/dev/null
}

cache_file=""
if [ "$mode" != "full" ]; then
  dir="${XDG_CACHE_HOME:-$HOME/.cache}/claude-pool-usage"
  id="$(printf '%s|%s' "$base" "$token" | { sha256sum 2>/dev/null || shasum -a 256 2>/dev/null || cksum; } | cut -c1-16)"
  cache_file="$dir/$id.txt"
  if [ -f "$cache_file" ]; then
    mtime="$(stat -c %Y "$cache_file" 2>/dev/null || stat -f %m "$cache_file" 2>/dev/null || echo 0)"
    age=$(( $(date +%s) - mtime ))
    if [ "$age" -lt "$cache_ttl" ]; then
      text="$(cat "$cache_file")"
    fi
  fi
fi

if [ -z "${text:-}" ]; then
  out="$(fetch)"
  code="${out##*$'\n'}"
  body="${out%$'\n'*}"
  body="${body%$'\n'}"
  case "$code" in
    200) text="$body" ;;
    401) fail "API key rejected" ;;
    404) fail "gateway has no /v1/pool-usage endpoint" ;;
    000|"") fail "gateway unreachable" ;;
    *)
      # Cloudflare Access login pages and other HTML mean the request never reached the gateway.
      case "$body" in *"<html"*|*"<!DOCTYPE"*) fail "blocked before gateway (HTTP $code)" ;; esac
      fail "HTTP $code" ;;
  esac
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
