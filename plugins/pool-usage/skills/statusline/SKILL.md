---
name: statusline
description: Install (or remove) the pool-usage status line, which keeps the shared pool's remaining 5-hour and weekly usage visible at the bottom of Claude Code.
disable-model-invocation: true
allowed-tools: Bash(mkdir *), Bash(cp *), Bash(chmod *), Read, Edit, Write
---

Set up the pool-usage status line for this user. Plugins cannot set `statusLine` themselves, so this writes it into the user's own settings.

1. Copy the script to a stable location, because the plugin directory changes on every plugin update:
   - Source: `${CLAUDE_PLUGIN_ROOT}/scripts/pool-usage.sh`
   - Destination: `~/.claude/pool-usage-statusline.sh` (use the user's real home path; on Windows that's the Git Bash home, e.g. `/c/Users/<name>/.claude/`)
   - Make it executable.

2. Read `~/.claude/settings.json` (create `{}` if missing) and look at the existing `statusLine` key:
   - If there is none, add:
     ```json
     "statusLine": {
       "type": "command",
       "command": "bash ~/.claude/pool-usage-statusline.sh line",
       "refreshInterval": 60
     }
     ```
   - If one already exists and is not ours, show it to the user and ask whether to replace it before changing anything.
   - Keep every other key in the file unchanged.

3. If the user asked to remove it instead, delete the `statusLine` entry only if its command points at `pool-usage-statusline.sh`, and delete that script file.

4. Tell the user it takes effect on the next status line refresh (or a new session), and that the line is cached for 30 seconds between gateway calls.

To update the copied script after a plugin update, run this command again.
