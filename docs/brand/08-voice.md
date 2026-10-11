# 08 · Voice

**Status: Approved** (three voice rules). Examples are **Specified**.

Crewbo talks like a calm, competent teammate who is good at coordination. It is short, specific and never dramatic.

## Rules

| Rule | Means | Not |
|---|---|---|
| **Plain over clever** | Say what is happening in words a tired teammate at 2 a.m. understands | Puns, jokes, jargon, invented terms |
| **Specific over vague** | Name the file, the person and the agent | "Something went wrong", "An error occurred" |
| **Calm, never alarming** | A blocked edit is a handoff, not an error. Offer the next step | "Error!", "Failed!", exclamation marks, red for non-conflicts |

Mechanics:

- Sentence case. No exclamation marks. No em dashes.
- Names in lower case as people typed them (`riya`), files in mono where possible (`token.go`).
- Present tense for live status (`Writing auth.go`), past tense for events (`riya handed off token.go`).
- Times in the user's local format (`9:40 pm`).
- Bo does not speak. Bo has no dialogue, no first-person copy ("Bo says…"). Crewbo speaks; Bo shows.
- Agents are named by product and model (`Claude Code · Opus 4.6`). Never invent a model name.

## Examples

| Situation | Write | Do not write |
|---|---|---|
| Handoff request | `arjun wants token.go next` · buttons `Hand off` / `Not yet` | `Conflict! arjun is blocked` |
| Claim | `claimed src/auth/** for 5m` | `Lock acquired successfully` |
| Blocked edit (agent) | `token.go is claimed by riya (Codex). Asked her to hand it off.` | `Permission denied` |
| Conflict | `riya and dev both changed Login() in auth.go. Choose which to keep.` | `Merge failed` |
| Merge | `Merged riya's rate limits into main` | `Woohoo! 🎉 Merged!` |
| Offline | `Offline since 9:40 pm` | `Disconnected!!` |
| Status bar | `3 online · 2 writing · 1 handoff · 0 conflicts` | `All systems go` |
| Task sent | `signed, waiting for approval` | `Task submitted!` |
| Empty room | `No one else is here yet. Share the room invite with your team.` | `It's lonely in here...` |

## Taglines

- Primary: **Every agent, one crew.**
- Supporting (social preview): *Your team's coding agents, working in parallel without collisions.*
