# Session handover: HANDOFF.md read at start, written at close

[Back to the README](../README.md)

![Session handover on the dashboard: the hooks' state, the briefing given at the start of a session, the writer's settings and instructions, and each repository's HANDOFF.md with its last activity. Example paths](screenshots/handover.png)

```mermaid
flowchart LR
  subgraph Close[Session ends]
    E["/exit, Ctrl-D, /clear<br>or the window closes"] --> J[Detached job]
    J --> W[Writer model re-reads<br>the finished session]
    W --> H[New dated section<br>at the top of HANDOFF.md]
    H --> G[Local commit of that one file<br>never pushed]
  end
  subgraph Start[Next session starts]
    S["New session or /clear"] --> R[Briefing: newest section,<br>commits since, git status, layout]
    R --> C[Claude skims the code it names<br>before the first answer]
  end
  G --> S
```

Two Claude Code hooks, installed and adapted from the dashboard's **Session handover** section. They only act in a repo whose git root has a `HANDOFF.md`: create one (even empty) to opt a repo in.

**Keep it local in a public repository.** Handover notes can name private repositories, machines and work in progress. Add `HANDOFF.md` to `.gitignore` and the hooks still read and write it, but never commit it: the writer skips the commit for an ignored file, and the briefing measures "commits since" from when the file was last written instead of its last commit.

- **SessionStart** (new session or `/clear`): Claude is given a briefing, the newest handover section, the commits since `HANDOFF.md` last changed, `git status` and the repo's layout, and told to skim the code it names before answering.
- **SessionEnd** (`/exit`, Ctrl-D, `/clear`, or closing the Ghostty window, which Claude Code reports as reason `other`): a detached job (`setsid`, so the window's SIGHUP cannot kill it) resumes a fork of the finished session with the writer model, has it prepend a dated section to `HANDOFF.md`, and commits that file locally (unless it is gitignored, or auto-commit is off), with `HANDOFF.archive.md` when a trim archived to it. It never pushes. Sessions with fewer typed prompts than the minimum are skipped, and so is a session with nothing worth handing over. An on-screen alert in the usage panel says when it is done.

The dashboard edits the briefing text, the writer's instructions, the writer model, the minimum prompts, whether to commit, how many sections to keep and whether to archive the rest, and shows the log. Settings: `~/.config/claude-burst/handover.json` (defaults stored as empty, so they follow new defaults). Scripts, the settings they read, the log and the writer's last reply: `~/.config/claude-burst/handover/`. The scripts are embedded in the binary and rewritten when they differ, so edit `internal/handover/scripts/`, not the installed copies.

## On the dashboard

| Control | What it does | Default |
| --- | --- | --- |
| **Reinstall hooks**, **Remove hooks** | Puts the two hooks in `~/.claude/settings.json`, or takes them out | installed from the dashboard |
| **Brief Claude from HANDOFF.md** | The start hook, and the text Claude is given | on |
| **Write the handover when a session closes** | The end hook | on |
| **Commit HANDOFF.md locally afterwards** | One local commit of that file (and its archive, when a trim archived to it), never pushed, skipped when the file is gitignored | on |
| **Minimum typed prompts** | Shorter sessions are skipped | 2 |
| **Sections to keep** | After each write, only the newest that many `# Handover, ` sections stay in `HANDOFF.md`; `0` keeps them all | 0 (all) |
| **Move trimmed sections to HANDOFF.archive.md** | The sections a trim removes go to the top of `HANDOFF.archive.md` instead of being deleted | off |
| **Writer model** | The model that writes the section | Opus |
| Writer's instructions | The rules for the section, with `{{file}}`, `{{now}}` and `{{session}}` filled in | built in |
| **Handover by repository** | Each repository the hooks acted in: last activity, size, writes, commits, with **History**, **View**, **Finder** and **Delete all** | |

The section's header says in one line what is installed. A half-installed pair of hooks is listed under **Needs attention** at the top of the dashboard.

## What a section looks like

```markdown
# Handover, 2026-10-07 14:20: retries for the order export
<!-- session: 4c1e9a02-... -->

## State right now
- Written at session close. Verify before acting.
- Pushed: retry with backoff in src/orders/export.ts. Local only: the config flag.

## What was done
- a1b2c3d: three attempts with backoff

## Open
- Make the backoff configurable. Check: make test

## Things that will bite you
- The export test needs the queue stub running.
```

Older sections stay below it, byte for byte.

## Keeping HANDOFF.md short

Left alone, `HANDOFF.md` grows by a section every session. Set **Sections to keep** and, after each session that changed the file, `write.sh` itself (not the model) keeps the newest that many `# Handover, ` sections and removes the older ones:

- Anything above the first section, and every block under another top-level heading (a hand-kept `# HANDOVER, project status` at the bottom, say), stays where it is. The match is case-sensitive, so `# HANDOVER,` is not a section.
- Two sections in a row with the same `<!-- session: ... -->` marker count as one, and headings inside code blocks are not sections.
- A session with nothing worth handing over leaves the file, and the archive, untouched.

With **Move trimmed sections to HANDOFF.archive.md** on, what was removed goes to the top of `HANDOFF.archive.md` beside `HANDOFF.md`, unchanged, so `HANDOFF.md` followed by the archive reads newest to oldest. Turn it on where git is no safety net: a gitignored `HANDOFF.md`, or auto-commit off. The archive follows `HANDOFF.md` in git: when `HANDOFF.md` is gitignored it is kept out of git too (through `.git/info/exclude`; `.gitignore` is never edited), and when the writer commits `HANDOFF.md` the same commit holds the archive. The archive is written before `HANDOFF.md`, each through a temporary file, so an interrupted trim never loses a section; a trim that fails leaves `HANDOFF.md` whole and logs `FAILED`. The briefing never reads the archive, and **Delete all** leaves it in place.

**Cost**: the writer re-reads the whole session, so a long session costs about one more turn of it, and it reads `HANDOFF.md` to add its section, so a long file costs on every write; **Sections to keep** bounds that. Pick a cheaper writer model to spend less.
