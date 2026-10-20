## ADDED Requirements

### Requirement: MCP responses name the resolved repo

When an MCP verb resolves a source repo (from `task_id`, `path`, or self-discovery), iris SHALL name that repo in the tool response as `repo: {name, path}`. `path` SHALL be the canonical source-repo path. `name` SHALL be the `owner/name` parsed from the source repo's `origin` remote URL, and SHALL be omitted when the URL can't be parsed as a GitHub-style remote. A response body that parses as JSON SHALL never be appended to. A JSON object body without a top-level `repo` key, whether success or error (for example `set_local_config`'s `{code, field, message, hint}` error envelope), SHALL carry `repo` as its first top-level key, with every existing field unchanged. Any other JSON body, including an object that already has `repo`, SHALL be returned unchanged. A plain-text body, success or error, SHALL end with a line `repo: <name> (<path>)`, or `repo: <path>` when there is no name, and its existing text (such as check or build output) SHALL be preserved byte for byte. A verb that resolves no repo SHALL return its response unchanged. If a verb resolves more than once, the first resolution SHALL be the one reported.

#### Scenario: Success response names the repo

- **WHEN** an agent calls `iris_gh_pr_merge` with a `task_id` whose source repo has origin `git@github.com:acme/video-gen.git`
- **THEN** the JSON response contains `"repo": {"name": "acme/video-gen", "path": "<canonical source repo path>"}` as its first key alongside the verb's own fields

#### Scenario: Error response names the repo

- **WHEN** a verb resolves its repo and then fails with a plain-text error (for example gh refuses the merge)
- **THEN** the error text is unchanged and followed by a final line `repo: acme/video-gen (<canonical source repo path>)`

#### Scenario: JSON error envelope stays decodable

- **WHEN** `iris_set_local_config` resolves its repo and then refuses with its JSON error envelope
- **THEN** the response is still a JSON object with its original `code`, `field`, `message` and `hint`, plus `repo` as its first key, and nothing is appended after it

#### Scenario: JSON body that already has repo is unchanged

- **WHEN** a resolving verb returns a JSON object that already has a top-level `repo` key, or a JSON value that is not an object
- **THEN** the response body is returned unchanged

#### Scenario: Unparseable origin omits the name

- **WHEN** the source repo's origin is a local filesystem path
- **THEN** the response's `repo` object contains `path` and no `name`

#### Scenario: Verb that resolves nothing is unchanged

- **WHEN** an agent calls `iris_ls`, or a verb fails input validation before resolving
- **THEN** the response carries no `repo` field or line
