# Fork releases: syncing upstream, signing, installing

[Back to the README](../README.md)

This fork (`RiaanLaubscher/claude-burst`) releases on its own. Fixes land here, not upstream. Releases are signed with the fork's key, and the installed copy upgrades from the fork.

## Layout

| Checkout | Purpose | Remotes |
| --- | --- | --- |
| `~/code/claude-burst` | working copy: branches, PRs, signing | `fork` = RiaanLaubscher, `origin` = andrewbakercloudscale |
| `~/claude-burst` | the installed copy (`~/.local/share/claude-burst/repo` points here) | `origin` = RiaanLaubscher, `upstream` = andrewbakercloudscale |

`scripts/release-signers` trusts two keys:

- `release@claude-burst`: Andrew Baker's key. It stays because the usage panel (`claude-code-cost-sidebar`) still comes from his repo and is verified against this file.
- `riaan@claude-burst`: this fork's key, `~/.ssh/claude-burst-release_ed25519` (`SHA256:An64s+h4GvKA/euY6Ux5dILJ4Qx2KAr3qjQCKcNB5U0`). It has a passphrase.

## 1. Sync upstream

Fork main has commits upstream doesn't have, so `gh repo sync` refuses to sync. **Never use `gh repo sync --force`**: it resets fork main to upstream and throws away the fork's fixes. Merge upstream through a PR instead:

```bash
cd ~/code/claude-burst
git fetch --multiple fork origin
git switch -c sync/upstream-$(date +%Y%m%d) fork/main
git merge origin/main
```

Conflicts to expect, and what to keep:

- `scripts/release-signers`: keep both keys, plus any new line upstream adds (for example, Andrew rotating his key).
- `scripts/reinstall.sh`, `scripts/repair.sh`, `internal/integration/reinstall_test.go`: keep the `RiaanLaubscher/claude-burst` URLs.
- `cmd/claude-burst/main.go` `const version`: leave it for step 3.
- Anything the fork has already fixed differently: compare both versions, then keep one and drop the other.

Test, then open the PR:

```bash
go vet ./...
env -u CLAUDE_BURST_REPO go test ./... -race -timeout 5m
git push -u fork HEAD
gh pr create -R RiaanLaubscher/claude-burst --base main
```

`env -u CLAUDE_BURST_REPO` matters: if that variable leaks in from an upgrade shell, the update-panel integration tests fail.

## 2. Fix or change something

Branch from `fork/main`, then commit, test as above, and open a PR to `RiaanLaubscher/claude-burst`. Don't open PRs against upstream.

## 3. Pick the version

The installer offers the **highest** `v*` tag merged into `origin/main`. Upstream tags count too: `~/claude-burst` has them from `upstream`, and they're signed by a trusted key. So a fork release must number above every upstream tag. Otherwise, once that upstream commit is merged into fork main, the upstream tag gets offered instead.

```bash
git ls-remote --tags --refs origin 'v*' | sed 's#.*/##' | sort -V | tail -1   # upstream's newest
git ls-remote --tags --refs fork 'v*'   | sed 's#.*/##' | sort -V | tail -1   # fork's newest
```

Take the higher of the two plus one patch. Set it in `cmd/claude-burst/main.go` (`const version = "X.Y.Z"`) on a branch, then PR and merge.

## 4. Sign the tag

Load the key once per login session, in a real terminal (the passphrase prompt needs a TTY):

```bash
ssh-add ~/.ssh/claude-burst-release_ed25519
```

Then, on merged fork main:

```bash
cd ~/code/claude-burst
git fetch fork && git switch --detach fork/main
env -u CLAUDE_BURST_REPO go test ./... -race -timeout 5m
bash scripts/sign-release.sh vX.Y.Z "$(git rev-parse HEAD)"
git push fork vX.Y.Z
```

`sign-release.sh` refuses a key that isn't in `release-signers`. It checks the new tag the way installed copies will, and deletes the tag if it doesn't verify or isn't the newest release.

## 5. Install

The usual route is the dashboard's **Upgrade** button. It fetches `origin` (the fork) in `~/claude-burst` and installs only a tag that verifies against the installed copy's `release-signers`.

By hand:

```bash
cd ~/claude-burst
git fetch --tags origin main
bash scripts/verify-release.sh "$PWD" origin/main      # prints "<tag> <commit>"
git checkout vX.Y.Z
env -u CLAUDE_BURST_REPO bash scripts/deploy.sh
claude-burst --version
curl -s http://127.0.0.1:7777/healthz
```

Changing `release-signers` only takes effect on a machine after that machine installs a release containing the change. The copy already running verifies with the file it has. So a new key needs one manual install (the steps above), the same way the move from upstream to this fork did.

## Rollback

Check out the previous tag in `~/claude-burst` and run `env -u CLAUDE_BURST_REPO bash scripts/deploy.sh`. If Burst itself is broken, run `burst-off` to take it out of the path. Full undo and emergency recovery are in [ROLLBACK.md](../ROLLBACK.md).
