# Public readiness (M6-01)

Checked on 2026-10-18 before `crewbo/crewbo` goes public for the M6 dogfood (ADR 0011). `crewbo/spikes` stays private; links to it from these docs will not open for outsiders.

## Secret scan

Every commit on every branch (`git log -p --all` after `git fetch --all`, 290 commits, about 5 MB of patches), searched for GitHub tokens, OpenAI and Anthropic keys, AWS keys, private key blocks, JWTs, Slack and Google keys, Crewbo session tokens (`cbs_`), invite codes, dashboard token addresses, quick tunnel addresses, email addresses and home paths.

| Pattern | Found | Verdict |
| --- | --- | --- |
| Tokens, keys, private keys, JWTs | None | Clean |
| `sk-...` | `sk-must-not-reach-the-agent`, a test placeholder | Fine |
| Quick tunnel address | `https://x.trycloudflare.com`, a placeholder | Fine |
| Session tokens, invite codes, dashboard tokens | None | Clean |
| Email addresses | Ronel's address as author and committer of all commits (metadata only, in no file); `@example.com` test addresses | Accepted by Ronel; future commits use the GitHub no-reply address |
| Home paths | `/home/ronel/...` in two docs; scrubbed placeholders (`/home/user`, `/home/u`, `/Users/u`) elsewhere | Fine (Ronel's own name) |

The GitHub OAuth app's client ID is in the code and docs. Device-flow client IDs are public by design; there is no client secret.

## Personal data

| Item | Where | Verdict |
| --- | --- | --- |
| Interview results | ADR 0013, PRD, decision log, worklog | Totals only (10 teams, 41 members, vendor counts); no names. Fine |
| `gavriel953` (a teammate's GitHub login) | Check reports, worklog, about 120 test lines as a sample member | Ronel asks him; if he declines, the login is replaced with a placeholder in current files before going public |
| Ronel's laptop model and OS | Phase 0 findings | Fine |
| University network | M4 check ("a university network"), no name | Fine |

## Licences

- `internal/server/LICENSE`: the AGPL-3.0 text from https://www.gnu.org/licenses/agpl-3.0.txt (661 lines, fetched 2026-10-18, SHA-256 `0d96a4ff68ad6d4b6f1f30f713b18d5184912ba8dd389f86aa7710db079abcb0`).
- `LICENSE` (Apache-2.0) stays at the root; `NOTICE` and the README say which covers what, and that the binary is AGPL-3.0 as a whole.
- Fonts: SIL Open Font License 1.1, licence files beside them.

## Before Ronel flips the repository to public

- [ ] gavriel953 has said yes to being named, or his login is replaced in current files
- [ ] `CODE_OF_CONDUCT.md`: replace `[INSERT CONTACT METHOD]` with a real contact
- [ ] Repository settings: turn on private vulnerability reporting (`SECURITY.md` points to it)
- [ ] Contributor licence: choose the CLA text, add a CLA bot for `internal/server/`, and a DCO check for every pull request (both are GitHub apps, org settings)
- [ ] Commit email: `git config --global user.email <id>+RM1338@users.noreply.github.com` and GitHub's "Block command line pushes that expose my email"
- [ ] Once public, branch protection works on GitHub Free: protect `main` (required review, required CI) as `docs/engineering/repo-settings.md` planned
- [ ] Flip the visibility (Settings, General, Danger Zone)
