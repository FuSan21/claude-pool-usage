---
name: statusline
description: Add (or remove) the pool usage in the Claude Code status bar. With claude-hud it shows as claude-hud's own 5h/7d usage bars; any other existing status line is kept and the pool line is added under it.
disable-model-invocation: true
allowed-tools: Bash(mkdir *), Bash(cp *), Bash(chmod *), Bash(rm *), Read, Edit, Write
---

Set up the pool usage status line. Plugins cannot set `statusLine` themselves, so this edits the user's own settings. Claude Code has a single status line command, so an existing one is chained, not replaced.

`<config>` is `~/.claude` unless `CLAUDE_CONFIG_DIR` is set. Use the user's real home path (on Windows, the Git Bash form such as `/c/Users/<name>/.claude` for shell commands, and `C:/Users/<name>/.claude` inside JSON).

## Install

1. Copy `${CLAUDE_PLUGIN_ROOT}/scripts/pool-usage.sh` to `<config>/pool-usage-statusline.sh` and make it executable. A stable copy is needed because the plugin directory changes on every update.

2. Read `<config>/settings.json` (missing file = `{}`) and check `statusLine`:
   - **None:** set
     ```json
     "statusLine": { "type": "command", "command": "bash ~/.claude/pool-usage-statusline.sh line", "refreshInterval": 60 }
     ```
   - **Already ours** (command contains `pool-usage-statusline.sh`): leave it; step 1 refreshed the script. Still do step 3 if claude-hud is the chained command.
   - **Someone else's:** chain it.
     1. Write its `command` string exactly as-is to `<config>/pool-usage-statusline.prev` (plain text, no JSON).
     2. Change only `command` to `bash ~/.claude/pool-usage-statusline.sh wrap`. Keep every other field; add `"refreshInterval": 60` only if none is set.
   - Keep every other key in settings.json unchanged.

3. **If the chained command is claude-hud** (it mentions `claude-hud`):
   1. Create the directory `<config>/pool-usage/`. Its presence tells the script to feed claude-hud instead of printing its own line.
   2. In `<config>/plugins/claude-hud/config.json`, under `display`, set `"externalUsagePath"` to the absolute path of `<config>/pool-usage/hud-usage.json`, and `"sevenDayThreshold": 0` so the weekly bar always shows. Keep everything else.
   3. Result: claude-hud shows the pool as its own Usage bars (5h and weekly), right next to Context.
   Otherwise the pool line is printed under the existing status line.

4. Tell the user it takes effect on the next refresh or new session; pool numbers refresh every 30 seconds.

## Remove

- If `<config>/pool-usage-statusline.prev` exists, put its contents back as `statusLine.command` (keep other fields) and delete the `.prev` file. Otherwise delete `statusLine` if its command contains `pool-usage-statusline.sh`.
- If `<config>/pool-usage/` exists: delete it, and remove `externalUsagePath` from claude-hud's config.json if it points there.
- Delete `<config>/pool-usage-statusline.sh`.

## Update

After a plugin update, run this command again. It refreshes the script copy and leaves the settings as they are.
