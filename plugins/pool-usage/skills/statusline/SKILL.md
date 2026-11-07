---
name: statusline
description: Add (or remove) the pool-usage line in the Claude Code status bar. Keeps any existing status line (such as claude-hud) and adds the pool line under it.
disable-model-invocation: true
allowed-tools: Bash(mkdir *), Bash(cp *), Bash(chmod *), Bash(rm *), Read, Edit, Write
---

Set up the pool-usage status line. Plugins cannot set `statusLine` themselves, so this edits the user's own `~/.claude/settings.json` (or `$CLAUDE_CONFIG_DIR/settings.json` if that is set). Claude Code has a single status line command, so an existing one is chained, not replaced.

Paths: `<config>` is `~/.claude` unless `CLAUDE_CONFIG_DIR` is set. Use the user's real home path (on Windows, the Git Bash form such as `/c/Users/<name>/.claude`).

## Install

1. Copy `${CLAUDE_PLUGIN_ROOT}/scripts/pool-usage.sh` to `<config>/pool-usage-statusline.sh` and make it executable. A stable copy is needed because the plugin directory changes on every update.

2. Read `<config>/settings.json` (treat a missing file as `{}`) and check the existing `statusLine`:
   - **None:** set
     ```json
     "statusLine": { "type": "command", "command": "bash ~/.claude/pool-usage-statusline.sh line", "refreshInterval": 60 }
     ```
   - **Already ours** (its command contains `pool-usage-statusline.sh`): leave the entry alone. Only the script copy from step 1 was refreshed.
   - **Someone else's** (for example claude-hud): chain it.
     1. Write its `command` string, exactly as it is, to `<config>/pool-usage-statusline.prev` (just the command text, no JSON).
     2. Change only the `command` to `bash ~/.claude/pool-usage-statusline.sh wrap`. Keep `type`, `padding` and every other field. Add `"refreshInterval": 60` only if no refreshInterval is set.
     3. Tell the user the previous status line still runs first, and the pool line now appears under it.
   - Keep every other key in settings.json unchanged.

3. Tell the user it takes effect on the next status line refresh or new session, and that the pool line is cached for 30 seconds between gateway calls.

## Remove

If the user asks to remove it:
- If `<config>/pool-usage-statusline.prev` exists, put its contents back as the `statusLine.command` (keeping the other fields), then delete the `.prev` file.
- Otherwise, if the `statusLine` command contains `pool-usage-statusline.sh`, delete the `statusLine` entry.
- Delete `<config>/pool-usage-statusline.sh`.

## Update

After a plugin update, run this command again. It refreshes the script copy and leaves the settings as they are.
