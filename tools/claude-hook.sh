#!/bin/sh
# Scouter: report this Claude Code session's state (working / waiting / done)
# to the desk dashboard. Forwards only session_id, cwd and the event name,
# never the prompt or transcript. Runs in the background and always exits 0,
# so it can never slow down or break Claude Code.
DIR="$HOME/.config/scouter"
[ -r "$DIR/auth-header" ] && [ -r "$DIR/url" ] || exit 0
body=$(python3 -c 'import sys, json
d = json.load(sys.stdin)
print(json.dumps({k: d.get(k, "") for k in ("session_id", "cwd", "hook_event_name")}))' 2>/dev/null) || exit 0
( curl -s -m 2 -o /dev/null -X POST -H @"$DIR/auth-header" -H "Content-Type: application/json" \
    --data-binary "$body" "$(cat "$DIR/url")/v1/agent-events" & ) >/dev/null 2>&1
exit 0
