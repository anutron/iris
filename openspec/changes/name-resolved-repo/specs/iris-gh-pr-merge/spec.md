## ADDED Requirements

### Requirement: PR identity and expected-repo guard on merge

Before merging, `iris:gh_pr_merge` SHALL run `gh pr view <pr_number> --json url,title` in the source repo and SHALL return the PR's `pr_url` and `pr_title` in its result. The verb SHALL accept an optional `expected_repo` string of the form `owner/name`. When `expected_repo` is given, the verb SHALL compare it case-insensitively with the `owner/name` parsed from the PR URL, and SHALL refuse without invoking `gh pr merge` if they differ or if the PR can't be identified. When `expected_repo` is omitted and the lookup fails, the verb SHALL proceed with the merge and add a warning to the result. An `expected_repo` not of the form `owner/name` SHALL be rejected before gh is invoked.

#### Scenario: Merge reports the PR it merged

- **WHEN** the verb merges PR 11 and gh reports its URL as `https://github.com/acme/video-gen/pull/11` with title `Add thumbnails`
- **THEN** the result includes `pr_url: "https://github.com/acme/video-gen/pull/11"` and `pr_title: "Add thumbnails"`

#### Scenario: Matching expected_repo merges

- **WHEN** the verb is invoked with `expected_repo="ACME/video-gen"` and the PR URL is under `acme/video-gen`
- **THEN** iris runs `gh pr merge` and returns `merged: true`

#### Scenario: Mismatched expected_repo refuses before merging

- **WHEN** the verb is invoked with `expected_repo="acme/sketch"` and the PR URL is under `acme/video-gen`
- **THEN** iris returns an error naming both repos and the PR title, and does NOT run `gh pr merge`

#### Scenario: Unidentifiable PR with expected_repo refuses

- **WHEN** `expected_repo` is given and `gh pr view` fails or returns no URL
- **THEN** iris returns an error and does NOT run `gh pr merge`

#### Scenario: Lookup failure without expected_repo still merges

- **WHEN** `expected_repo` is omitted and `gh pr view` fails
- **THEN** iris runs `gh pr merge` as before and the result carries a warning that the PR could not be identified

#### Scenario: Malformed expected_repo is rejected before gh

- **WHEN** the verb is invoked with `expected_repo="video-gen"`
- **THEN** iris returns an error mentioning `expected_repo` and does not invoke gh
