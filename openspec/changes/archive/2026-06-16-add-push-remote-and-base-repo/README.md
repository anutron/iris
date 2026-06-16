# add-push-remote-and-base-repo

Add an optional `remote` to iris:push and an optional `base_repo` to iris:gh_pr_create so a maintainer can push a branch to a non-origin remote (e.g. the upstream they have write access to) and open a same-repo PR there — the CI-gated motion that the existing cross-fork auto-detection cannot express.
