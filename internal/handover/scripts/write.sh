#!/usr/bin/env bash
# Detached worker started by end.sh, installed by claude-burst (internal/handover).
# Generated: edits here are overwritten; change internal/handover/scripts/write.sh.
#
# Resumes a fork of the finished session (so the writer has the whole
# conversation), lets it update HANDOFF.md, then commits that file (and
# HANDOFF.archive.md, when a trim archived to it) locally.
# Never pushes.
set -uo pipefail
DIR="$(cd "$(dirname "$0")" && pwd)"
sid=$1 cwd=$2 root=$3
file="$root/HANDOFF.md"
out="$DIR/last-run.json"   # the writer's full reply, for when it goes wrong
say() { echo "$(date '+%Y-%m-%d %H:%M:%S') $* [$sid]"; }
# An on-screen alert in the usage panel, through the gateway's dashboard. $2
# is the severity, ok unless given. Never fails the run: no gateway, no alert.
notify() {  # CLAUDE_HANDOVER_NO_NOTIFY is for the tests, which run this script for real
  [[ -n "${CLAUDE_HANDOVER_NO_NOTIFY:-}" ]] && return 0
  local dash body
  dash=$(python3 -c 'import json,os;print(json.load(open(os.path.expanduser("~/.config/claude-burst/config.json"))).get("admin_listen") or "127.0.0.1:7788")' 2>/dev/null || echo 127.0.0.1:7788)
  [[ "$dash" == off ]] && return 0
  body=$(python3 -c 'import json,sys;print(json.dumps({"kind":"handover","severity":sys.argv[2],"title":"Claude handover","detail":sys.argv[1]}))' "$1" "${2:-ok}") || return 0
  curl -s -m 3 -o /dev/null -X POST -H 'X-Claude-Burst-Admin: 1' -H 'Content-Type: application/json' -d "$body" "http://$dash/api/alert" || true
}

trim_sections() {
  python3 - "$@" <<'PY'
import os, re, sys, tempfile

path, apath, keep, archive = os.path.realpath(sys.argv[1]), os.path.realpath(sys.argv[2]), int(sys.argv[3]), sys.argv[4] == "1"
fence_re = re.compile(r" {0,3}(`{3,}|~{3,})(.*)")
marker_re = re.compile(r"<!-- session: (.+?) -->$")


def read(p):
    with open(p, encoding="utf-8", errors="surrogateescape", newline="") as f:
        return f.read()


def put(p, text, mode):
    fd, tmp = tempfile.mkstemp(dir=os.path.dirname(p), prefix="." + os.path.basename(p) + ".", suffix=".tmp")
    try:
        with os.fdopen(fd, "w", encoding="utf-8", errors="surrogateescape", newline="") as f:
            f.write(text)
            f.flush()
            os.fsync(f.fileno())
        os.chmod(tmp, mode)
        os.replace(tmp, p)
    except BaseException:
        try:
            os.unlink(tmp)
        except OSError:
            pass
        raise


text = read(path)
lines = [line + "\n" for line in text.split("\n")]
lines[-1] = lines[-1][:-1]
blocks = [{"handover": False, "key": None, "lines": [], "looked": True}]
fence = None
for line in lines:
    m = fence_re.match(line)
    if fence:
        if m and m.group(1)[0] == fence[0] and len(m.group(1)) >= len(fence) and not m.group(2).strip():
            fence = None
    elif m:
        fence = m.group(1)
    elif line.startswith("# "):
        blocks.append({"handover": line.startswith("# Handover, "), "key": None, "lines": [line], "looked": False})
        continue
    b = blocks[-1]
    if not b["looked"] and line.strip():
        b["looked"] = True
        k = marker_re.match(line.strip())
        b["key"] = k.group(1) if k else None
    b["lines"].append(line)

kept, gone, sessions, prev, n_kept = [], [], 0, None, 0
for b in blocks:
    chunk = "".join(b["lines"])
    if not b["handover"]:
        kept.append(chunk)
        prev = None
        continue
    if b["key"] is None or b["key"] != prev:
        sessions += 1
    prev = b["key"]
    if sessions <= keep:
        kept.append(chunk)
        n_kept += 1
    else:
        gone.append(chunk)

if keep <= 0 or not gone:
    sys.exit(0)

mode = os.stat(path).st_mode & 0o7777
if archive:
    removed, old, amode = "".join(gone), "", mode
    if os.path.exists(apath):
        old, amode = read(apath), os.stat(apath).st_mode & 0o7777
    if old and not removed.endswith("\n"):
        removed += "\n"
    put(apath, removed + old, amode)
put(path, "".join(kept), mode)
done = "archived %d to HANDOFF.archive.md" % len(gone) if archive else "removed %d" % len(gone)
print("kept %d section%s, %s" % (n_kept, "" if n_kept == 1 else "s", done))
PY
}

trim() {
  if [[ "$archive" == 1 ]] && git -C "$root" check-ignore -q HANDOFF.md 2>/dev/null &&
    ! git -C "$root" check-ignore -q HANDOFF.archive.md 2>/dev/null; then
    local exclude
    if ! exclude=$(git -C "$root" rev-parse --path-format=absolute --git-path info/exclude) ||
      ! mkdir -p "$(dirname "$exclude")" ||
      ! { [[ ! -s "$exclude" || -z "$(tail -c1 "$exclude")" ]] || echo >>"$exclude"; } ||
      ! echo "/HANDOFF.archive.md" >>"$exclude"; then
      say "FAILED $root: trim skipped, nothing removed: could not keep HANDOFF.archive.md out of git"
      return
    fi
  fi
  local res
  if res=$(trim_sections "$file" "$archive_file" "$keep" "$archive" 2>&1); then
    if [[ -n "$res" ]]; then say "trim   $root: $res"; fi
  else
    say "FAILED $root: trim failed, nothing removed: $(tail -c 300 <<<"$res" | tr '\n' ' ')"
  fi
}

# one writer per repo at a time: closing Ghostty ends every tab's session at once
lock="$root/.git/handover.lock"
for _ in $(seq 1 600); do mkdir "$lock" 2>/dev/null && break; sleep 1; done
[[ -d "$lock" ]] || { say "FAILED $root: gave up waiting for $lock"; exit 1; }
trap 'rmdir "$lock" 2>/dev/null' EXIT

now=$(date '+%Y-%m-%d %H:%M')
read -r model commit keep archive < <(python3 -c 'import json,sys; s=json.load(open(sys.argv[1])); print(s.get("model") or "opus", int(bool(s.get("commit"))), int(s.get("keep_sections") or 0), int(bool(s.get("archive_trimmed"))))' "$DIR/settings.json")
prompt=$(python3 -c 'import json,sys; t=json.load(open(sys.argv[1]))["instructions"]; print(t.replace("{{file}}", sys.argv[2]).replace("{{now}}", sys.argv[3]).replace("{{session}}", sys.argv[4]))' "$DIR/settings.json" "$file" "$now" "$sid")

archive_file="$root/HANDOFF.archive.md"
dirty_before=$(git -C "$root" status --porcelain -- HANDOFF.md)
archive_dirty_before=$(git -C "$root" status --porcelain -- HANDOFF.archive.md)
before=$(shasum "$file" | cut -d' ' -f1)
say "write  $root with $model"

cd "$cwd" || { say "FAILED $root: $cwd is gone"; exit 1; }
# The prompt goes on stdin: --allowedTools is variadic and swallows a trailing
# prompt argument, which then fails with a confusing "deferred tool marker" error.
CLAUDE_HANDOVER_WRITER=1 /usr/bin/perl -e 'alarm 900; exec @ARGV' claude -p \
  --resume "$sid" --fork-session --model "$model" \
  --permission-mode acceptEdits \
  --allowedTools="Read,Edit,Write,Grep,Glob,Bash(git log:*),Bash(git status:*),Bash(git diff:*),Bash(git show:*)" \
  --output-format json <<<"$prompt" >"$out" 2>&1
rc=$?

# The instructions forbid em and en dashes and a model still wrote one in
# testing. Older sections are already dash-free, so a whole-file pass only
# touches the new one.
[[ $rc -eq 0 ]] && python3 -c 'import sys; p=sys.argv[1]; s=open(p).read(); t=s.replace(" \u2014 ", ", ").replace("\u2014", ", ").replace("\u2013", "-"); t!=s and open(p,"w").write(t)' "$file"
if [[ $rc -eq 0 && $keep -gt 0 && "$(shasum "$file" | cut -d' ' -f1)" != "$before" ]]; then trim; fi
after=$(shasum "$file" | cut -d' ' -f1)

if [[ $rc -ne 0 ]]; then
  say "FAILED $root: claude exited $rc: $(tail -c 300 "$out" | tr '\n' ' ')"
  notify "Handover FAILED for $(basename "$root"), see the claude-burst dashboard" error; exit 1
fi
if [[ "$before" == "$after" ]]; then
  say "none   $root: nothing worth handing over"; exit 0
fi
if [[ "$commit" != 1 ]]; then
  say "wrote  $root (not committed: auto-commit is off)"
  notify "Handover updated in $(basename "$root")"; exit 0
fi
# A gitignored HANDOFF.md is local notes by choice (a public repo should not
# publish them), so there is nothing to commit.
if git -C "$root" check-ignore -q HANDOFF.md 2>/dev/null; then
  say "wrote  $root (local only: HANDOFF.md is gitignored)"
  notify "Handover updated in $(basename "$root")"; exit 0
fi
if [[ -n "$dirty_before" ]]; then
  say "wrote  $root (not committed: HANDOFF.md already had uncommitted edits)"
  notify "Handover updated in $(basename "$root"), left uncommitted"; exit 0
fi
paths=(HANDOFF.md)
if [[ "$archive" == 1 && -z "$archive_dirty_before" && -n "$(git -C "$root" status --porcelain -- HANDOFF.archive.md)" ]] &&
  git -C "$root" add -- HANDOFF.archive.md; then
  paths+=(HANDOFF.archive.md)
fi
if git -C "$root" commit -q -m "Update the handover at session close ($now)" -- "${paths[@]}"; then
  say "wrote  $root, committed $(git -C "$root" rev-parse --short HEAD)"
else
  if [[ ${#paths[@]} -gt 1 ]]; then git -C "$root" reset -q -- HANDOFF.archive.md; fi
  say "wrote  $root (commit failed)"
fi
notify "Handover updated in $(basename "$root")"
