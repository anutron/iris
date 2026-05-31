## 1. Skill content

- [x] 1.1 Write `claude/skills/iris/config-authoring.md`: the two files + overlay relationship; field taxonomy table (shared→`.iris.toml`, local→`.iris.local.toml`) and misplacement warnings; `[build]`/`[restart]`/hook blocks; the six restart mechanisms with the `exit_code`-self-only rule; the `iris_validate_config` + `iris_set_local_config` authoring loop; worked examples; common mistakes.
- [x] 1.2 Update `claude/skills/iris/SKILL.md`: add `iris_set_local_config` to the self-management list; add an "Authoring `.iris.toml` / `.iris.local.toml`" subsection that gives a 30-second orientation and points to `config-authoring.md`.

## 2. Validation

- [x] 2.1 Cross-check every documented field and restart mechanism against `internal/config/iris_toml.go` and `iris_toml_taxonomy.go` so the reference matches the parser.
- [x] 2.2 `openspec validate teach-iris-config-authoring --strict` clean.
- [x] 2.3 `openspec validate --all --strict` clean.
- [x] 2.4 `bash claude/install_test.sh` still green (the content-gate assertions in it are unaffected; the symlinked skill dir now also carries `config-authoring.md`).
