---
name: pool-usage
description: Show how much of the shared Claude account pool's 5-hour and weekly usage is left for your API key. Use when the user asks about remaining usage, limits, quota, or when the pool resets.
allowed-tools: Bash(bash *pool-usage.sh*)
---

Current pool usage for this API key:

```!
bash "${CLAUDE_PLUGIN_ROOT}/scripts/pool-usage.sh" full
```

Show the output above to the user as-is in a code block. Then add one short line explaining it: "left" is the combined remaining allowance across all accounts this key can use, and the reset time is the soonest window that frees up capacity. If the output is an error, explain it briefly (for example, the key was rejected or the gateway is unreachable).
