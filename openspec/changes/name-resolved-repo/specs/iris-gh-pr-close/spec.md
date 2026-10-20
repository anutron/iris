## ADDED Requirements

### Requirement: PR identity and expected-repo guard on close

Before closing, `iris:gh_pr_close` SHALL run `gh pr view <pr_number> --json url,title` in the source repo and SHALL return the PR's `pr_url` and `pr_title` in its result. The verb SHALL accept an optional `expected_repo` string of the form `owner/name`. When `expected_repo` is given, the verb SHALL compare it case-insensitively with the `owner/name` parsed from the PR URL, and SHALL refuse without invoking `gh pr close` if they differ or if the PR can't be identified. When `expected_repo` is omitted and the lookup fails, the verb SHALL proceed with the close and leave `pr_url` and `pr_title` empty. An `expected_repo` not of the form `owner/name` SHALL be rejected before gh is invoked.

#### Scenario: Close reports the PR it closed

- **WHEN** the verb closes PR 12 and gh reports its URL as `https://github.com/acme/video-gen/pull/12`
- **THEN** the result includes that `pr_url` and the PR's `pr_title`

#### Scenario: Mismatched expected_repo refuses before closing

- **WHEN** the verb is invoked with `expected_repo="acme/sketch"` and the PR URL is under `acme/video-gen`
- **THEN** iris returns an error naming both repos and does NOT run `gh pr close`

#### Scenario: Lookup failure without expected_repo still closes

- **WHEN** `expected_repo` is omitted and `gh pr view` fails
- **THEN** iris runs `gh pr close` as before and returns `closed: true` with empty `pr_url`
