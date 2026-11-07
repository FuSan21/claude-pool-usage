# claude-pool-usage

Shows, inside Claude Code, how much of a shared Claude account pool is left:
the combined **5-hour** and **weekly** allowance of every upstream account your
API key can use on a [sub2api](https://github.com/Wei-Shaw/sub2api) gateway.

```
5h 79% left (reset 2h38m) · 7d 41% left (reset 15h58m) · 3 accts
```

It has two parts:

| Part | What it is |
|---|---|
| `plugins/pool-usage` | Claude Code plugin: `/pool-usage` command, a session-start notice, and an optional status line |
| `server/` | Small read-only service that adds `GET /v1/pool-usage` next to sub2api |

## Install the plugin

Claude Code must already point at the gateway (`ANTHROPIC_BASE_URL`, plus
`ANTHROPIC_AUTH_TOKEN` with your sub2api key, and `ANTHROPIC_CUSTOM_HEADERS` if
the gateway sits behind Cloudflare Access). The plugin reuses those settings;
there is nothing else to configure.

```
/plugin marketplace add FuSan21/claude-pool-usage
/plugin install pool-usage@claude-pool-usage
```

Then:

- **`/pool-usage`** shows the pool total and a per-account breakdown.
- A one-line notice appears when a session starts.
- **`/pool-usage:statusline`** (optional) adds the line to your status bar.
  Plugins can't set the status line themselves, so this copies the script to
  `~/.claude/pool-usage-statusline.sh` and adds a `statusLine` entry to your
  `~/.claude/settings.json`. Run it again after plugin updates.

Requires `bash` and `curl` (on Windows, the Git Bash that Claude Code uses).

## How "left" is computed

Your key's group decides which accounts count. For each active, schedulable
account:

- remaining = 1 − utilization of the current window (a window that has already
  reset counts as fully available);
- an account that is rate-limited or cooling down counts as 0 for the 5-hour window;
- an account that has not served a request yet has no data and counts as unused
  (shown as "unsampled").

The pool figure is the sum of remaining, as a percentage of the account count.
The reset time is the soonest reset among accounts that have used something.
Utilization comes from Anthropic's rate-limit headers, which sub2api records on
every response, so an account's numbers are as fresh as its latest request.

## Server

`GET /v1/pool-usage` authenticates with the same API key (`x-api-key` or
`Authorization: Bearer`). Responses: JSON by default, `?format=text` for the one
line, `?format=full` for the breakdown. Results are cached per group for 15s.
Account emails are never returned.

It needs a read-only Postgres role on the sub2api database:

```sql
CREATE ROLE pool_usage_ro LOGIN PASSWORD '...';
ALTER ROLE pool_usage_ro SET default_transaction_read_only = on;
GRANT CONNECT ON DATABASE sub2api TO pool_usage_ro;
GRANT USAGE ON SCHEMA public TO pool_usage_ro;
GRANT SELECT (id, key, user_id, group_id, status, expires_at, deleted_at) ON api_keys TO pool_usage_ro;
GRANT SELECT (id, status, deleted_at) ON users TO pool_usage_ro;
GRANT SELECT (id, name, deleted_at) ON groups TO pool_usage_ro;
GRANT SELECT (account_id, group_id) ON account_groups TO pool_usage_ro;
GRANT SELECT (id, status, schedulable, priority, deleted_at, rate_limit_reset_at,
  temp_unschedulable_until, overload_until, session_window_end, extra) ON accounts TO pool_usage_ro;
```

Add it to the sub2api compose and route only `/v1/pool-usage` to it:

```yaml
  pool-usage:
    build: ./server            # or a prebuilt image
    restart: unless-stopped
    read_only: true
    environment:
      - DATABASE_URL=postgres://pool_usage_ro@postgres:5432/sub2api?sslmode=disable
      - DATABASE_PASSWORD_FILE=/run/secrets/db_password
    volumes:
      - ./db_password:/run/secrets/db_password:ro
```

Environment: `DATABASE_URL` (required), `DATABASE_PASSWORD_FILE`,
`LISTEN_ADDR` (default `:8090`), `CACHE_SECONDS` (default 15).

## License

MIT
