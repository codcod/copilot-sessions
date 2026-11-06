# Procedure: rework a ticket

When asked to rework ticket T-NNN (a review found blocking findings):

Under `layout = "in-tree"`, before reading the ticket, resolve its current status from the base
branch rather than trusting the worktree — `git ls-tree -r --name-only <base> -- tickets |
grep -- "/T-NNN-"`, then `git show <base>:<that path>` — and, once any pre-existing
`feat/T-NNN-*` branch for this ticket is checked out (a resumed pickup; a fresh one has no branch
yet), run `pickle doctor` and resolve any stale-ticket-branch warning first.

1. The ticket must be in `5-rework/` on the base branch (per the lookup above) — if not, stop and
   explain.
2. Read the ticket's `## Review` section: the blocking findings are the entire scope.
   Implement nothing else — any new work needs a new ticket.
3. On the same `feat/T-NNN-<slug>` branch (in the target child's repo), fix only the listed
   findings (local commits per the commit policy — they make the re-review diffable). Note the
   branch tip before your first fix commit: that is what the re-review diffs against.
4. Re-run the acceptance test and the child's build/validate commands until green.
5. Record what was fixed against each finding in `## Review`, and re-read the replacement text
   you just wrote before the re-review — you are its cheapest reader. Head the record
   `### Rework fix record — round N (commit <sha>)` for a single commit, or
   `(commits <the tip you noted in step 3>..<tip after>)` for several — the form
   `git diff <before>..<after>` takes as written — or `no commits this round — <why>` for none.
   Record the SHAs as they stand when the re-review starts, and do not tidy the branch here: the
   tidy belongs to publishing, and it rewrites them (§1's fallback covers a record whose SHAs no
   longer resolve). The scoped re-review reads that diff (`resources/review-protocol.md` §1).
6. `pickle ticket move T-NNN in-review --reason "findings fixed"`. On a host that can spawn
   sub-agents, continue in this session into the scoped re-review (*Procedure: validate a
   ticket*) as its orchestrator. Spawn the reviewer with the *Spawn prompt* in
   `resources/review-protocol.md` step 0 — read it there and copy it verbatim, filling only its
   `<…>` fields, and add nothing to it, not even what the fix changed.
   New blocking findings start another round from step 2 — at most two rework rounds per
   invocation, a round being one fix record written in it; each "rework ticket T-NNN" starts
   the count afresh. When the second round's re-review still has blocking findings, the review
   moves the ticket to `5-rework/` as usual and the session stops, summarising the open findings.
   A clean re-review carries on to the review's approval presentation (its step 9) and stops
   there. Without sub-agents, hand back for a scoped re-review in a fresh session.
