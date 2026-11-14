## 1. Specs

- [x] 1.1 Delta specs for iris-gh-pr-merge, iris-checkout, iris-cherry-pick, iris-merge-to-master, iris-dogfood-lease, iris-status

## 2. Tests (write first)

- [ ] 2.1 gh_pr_merge: host on dev branch stays put and local main fast-forwards
- [ ] 2.2 gh_pr_merge: host on main fast-forwards in place
- [ ] 2.3 gh_pr_merge: diverged local main warns and is left alone
- [ ] 2.4 gh_pr_merge: serializes with other source-repo verbs
- [ ] 2.5 checkout / cherry_pick / merge_to_master / complete_task: lease refusal, holder pass, override, no-dogfood-branch pass
- [ ] 2.6 status: host_off_dogfood

## 3. Implementation

- [ ] 3.1 host_move.go gate helper
- [ ] 3.2 gh_pr_merge post-merge sync
- [ ] 3.3 Wire the gate into checkout, cherry_pick, merge_to_master, complete_task
- [ ] 3.4 status fields
- [ ] 3.5 MCP handlers and tool catalog (`lease_override`)

## 4. Docs and cleanup

- [ ] 4.1 claude/skills/iris/SKILL.md
- [ ] 4.2 Remove .workflow/todo/gh-pr-merge-unlocked-post-merge-checkout.md
- [ ] 4.3 make test vet fmt build; openspec validate --all --strict; archive
