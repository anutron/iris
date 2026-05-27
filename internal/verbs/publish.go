// Package verbs: publish implements iris:publish — update the source repo's
// currently-checked-out branch to the worktree's HEAD, optionally push to
// origin, then rebuild and restart by delegating to reload's helpers.
//
// See openspec/changes/add-publish-verb/design.md.

package verbs

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/config"
)

// PublishInput is the public input shape for iris:publish.
type PublishInput struct {
	TaskID string
	Branch string // optional; defaults to source repo's current branch
	Push   bool
	Reset  bool
	Caller string
}

// PublishResult is the structured result. The audit-log shape reuses
// AuditEntry from reload (with Mode="publish"); this struct is what the
// MCP/CLI callers see.
type PublishResult struct {
	TargetSourceRepo string   `json:"target_source_repo"`
	Mode             string   `json:"mode"`
	Branch           string   `json:"branch"`
	WorktreeSha      string   `json:"worktree_sha"`
	PrePublishSha    string   `json:"pre_publish_sha"`
	PostPublishSha   string   `json:"post_publish_sha"`
	Reset            bool     `json:"reset"`
	Pushed           bool     `json:"pushed"`
	RemoteSHA        string   `json:"remote_sha,omitempty"`
	BuildOutput      string   `json:"build_output"`
	RestartMechanism string   `json:"restart_mechanism"`
	RestartOutput    string   `json:"restart_output"`
	Warnings         []string `json:"warnings,omitempty"`
}

// Publish runs the publish sequence: pre-flight, lock, git update
// (ff-only or --reset), optional push, build, restart, audit.
func Publish(ctx context.Context, client *argus.Client, in PublishInput) (*PublishResult, error) {
	caller := in.Caller
	if caller == "" {
		caller = "unknown"
	}

	if strings.TrimSpace(in.TaskID) == "" {
		writeAudit(AuditEntry{
			Caller: caller, Mode: "publish", Outcome: "failure",
			FailureReason: "task_id is required",
		})
		return nil, fmt.Errorf("task_id is required")
	}

	// 1. Resolve worktree + source repo (allowlist enforced inside Resolve).
	resolved, err := Resolve(ctx, client, in.TaskID)
	if err != nil {
		writeAudit(AuditEntry{
			Caller: caller, Mode: "publish", Outcome: "failure",
			FailureReason: err.Error(),
		})
		return nil, err
	}
	src := resolved.SourceRepo
	wt := resolved.WorktreePath

	// 2. Pre-flight refusals (no lock yet).
	// 2a. Clean worktree.
	if err := checkCleanTree(ctx, wt); err != nil {
		err = fmt.Errorf("worktree %s: %w", wt, err)
		writeAudit(AuditEntry{
			Caller: caller, TargetSourceRepo: src, Mode: "publish", Outcome: "failure",
			FailureReason: err.Error(),
		})
		return nil, err
	}
	// 2b. Clean source repo.
	if err := checkCleanTree(ctx, src); err != nil {
		err = fmt.Errorf("source repo %s: %w", src, err)
		writeAudit(AuditEntry{
			Caller: caller, TargetSourceRepo: src, Mode: "publish", Outcome: "failure",
			FailureReason: err.Error(),
		})
		return nil, err
	}
	// 2c. .iris.toml loaded + validated.
	// Publish is always treated as cross-target for validation purposes: even if
	// the source repo happens to be iris's own, publish does not invoke the
	// self-exit choreography, so exit_code mechanism is refused here.
	tomlPath := filepath.Join(src, config.IrisTomlFilename)
	doc, verrs, err := config.LoadIrisToml(tomlPath, false)
	if err != nil {
		writeAudit(AuditEntry{
			Caller: caller, TargetSourceRepo: src, Mode: "publish", Outcome: "failure",
			FailureReason: err.Error(),
		})
		return nil, err
	}
	if len(verrs) > 0 {
		reason := joinValidationErrors(verrs)
		writeAudit(AuditEntry{
			Caller: caller, TargetSourceRepo: src, Mode: "publish", Outcome: "failure",
			FailureReason: reason,
		})
		return nil, fmt.Errorf("%s invalid: %s", config.IrisTomlFilename, reason)
	}
	// 2d. Target branch resolution + constraint check.
	srcBranch, err := currentBranch(ctx, src)
	if err != nil {
		writeAudit(AuditEntry{
			Caller: caller, TargetSourceRepo: src, Mode: "publish", Outcome: "failure",
			FailureReason: err.Error(),
		})
		return nil, fmt.Errorf("read source repo current branch: %w", err)
	}
	targetBranch := in.Branch
	if targetBranch == "" {
		targetBranch = srcBranch
	}
	if targetBranch != srcBranch {
		err := fmt.Errorf("target branch %q does not match source repo's current branch %q; v1.2 requires they match", targetBranch, srcBranch)
		writeAudit(AuditEntry{
			Caller: caller, TargetSourceRepo: src, Mode: "publish", Outcome: "failure",
			FailureReason: err.Error(),
		})
		return nil, err
	}
	// 2e. Capture worktree HEAD now (before lock so we fail fast on a broken worktree).
	wtSHA, err := runGit(ctx, wt, "rev-parse", "HEAD")
	if err != nil {
		writeAudit(AuditEntry{
			Caller: caller, TargetSourceRepo: src, Mode: "publish", Outcome: "failure",
			FailureReason: err.Error(),
		})
		return nil, fmt.Errorf("read worktree HEAD: %w", err)
	}
	wtSHA = strings.TrimSpace(wtSHA)

	// 3. Lock — every subsequent step must release before returning.
	mu := lockSourceRepo(src)
	lockHeld := true
	releaseLock := func() {
		if lockHeld {
			mu.Unlock()
			lockHeld = false
		}
	}
	defer releaseLock()

	// Capture source HEAD before the update so we have pre/post SHAs for the audit.
	preSHA, err := runGit(ctx, src, "rev-parse", "HEAD")
	if err != nil {
		writeAudit(AuditEntry{
			Caller: caller, TargetSourceRepo: src, Mode: "publish", Outcome: "failure",
			FailureReason: err.Error(),
		})
		return nil, fmt.Errorf("read source repo HEAD: %w", err)
	}
	preSHA = strings.TrimSpace(preSHA)

	// 4. Git update: ff-only by default; --reset opts into hard reset.
	if in.Reset {
		if out, err := runGit(ctx, src, "reset", "--hard", wtSHA); err != nil {
			reason := fmt.Errorf("git reset --hard %s: %w; log:\n%s", wtSHA, err, out)
			writeAudit(AuditEntry{
				Caller: caller, TargetSourceRepo: src, Mode: "publish", Outcome: "failure",
				PrePullSha: preSHA, PostPullSha: preSHA,
				FailureReason: reason.Error(),
			})
			return nil, reason
		}
	} else {
		if out, err := runGit(ctx, src, "merge", "--ff-only", wtSHA); err != nil {
			reason := fmt.Errorf("git merge --ff-only %s: %w; (pass --reset to allow non-fast-forward); log:\n%s", wtSHA, err, out)
			writeAudit(AuditEntry{
				Caller: caller, TargetSourceRepo: src, Mode: "publish", Outcome: "failure",
				PrePullSha: preSHA, PostPullSha: preSHA,
				FailureReason: reason.Error(),
			})
			return nil, reason
		}
	}
	postSHA, err := runGit(ctx, src, "rev-parse", "HEAD")
	if err != nil {
		writeAudit(AuditEntry{
			Caller: caller, TargetSourceRepo: src, Mode: "publish", Outcome: "failure",
			PrePullSha: preSHA,
			FailureReason: err.Error(),
		})
		return nil, fmt.Errorf("re-read source repo HEAD after update: %w", err)
	}
	postSHA = strings.TrimSpace(postSHA)

	// 5. Optional push (matches v1.0 push guardrails: refuses default branch).
	pushed := false
	remoteSHA := ""
	if in.Push {
		defaultBranch, err := DefaultBranch(ctx, src)
		if err != nil {
			writeAudit(AuditEntry{
				Caller: caller, TargetSourceRepo: src, Mode: "publish", Outcome: "failure",
				PrePullSha: preSHA, PostPullSha: postSHA,
				FailureReason: err.Error(),
			})
			return nil, fmt.Errorf("determine default branch: %w", err)
		}
		if targetBranch == defaultBranch {
			err := fmt.Errorf("refusing to push default branch %q (publish updated the local ref but did not push)", targetBranch)
			writeAudit(AuditEntry{
				Caller: caller, TargetSourceRepo: src, Mode: "publish", Outcome: "failure",
				PrePullSha: preSHA, PostPullSha: postSHA,
				FailureReason: err.Error(),
			})
			return nil, err
		}
		if out, err := runGit(ctx, src, "push", "origin", targetBranch); err != nil {
			reason := fmt.Errorf("git push origin %s: %w; log:\n%s", targetBranch, err, out)
			writeAudit(AuditEntry{
				Caller: caller, TargetSourceRepo: src, Mode: "publish", Outcome: "failure",
				PrePullSha: preSHA, PostPullSha: postSHA,
				FailureReason: reason.Error(),
			})
			return nil, reason
		}
		sha, err := runGit(ctx, src, "rev-parse", "origin/"+targetBranch)
		if err != nil {
			writeAudit(AuditEntry{
				Caller: caller, TargetSourceRepo: src, Mode: "publish", Outcome: "failure",
				PrePullSha: preSHA, PostPullSha: postSHA,
				FailureReason: err.Error(),
			})
			return nil, fmt.Errorf("rev-parse origin/%s: %w", targetBranch, err)
		}
		pushed = true
		remoteSHA = strings.TrimSpace(sha)
	}

	// 6. Build — delegate to reload's runBuildBlock.
	buildOutput, err := runBuildBlock(ctx, src, doc.Build)
	if err != nil {
		writeAudit(AuditEntry{
			Caller: caller, TargetSourceRepo: src, Mode: "publish", Outcome: "failure",
			PrePullSha: preSHA, PostPullSha: postSHA, BuildOutput: buildOutput,
			FailureReason: err.Error(),
		})
		return nil, err
	}

	// 7. Restart dispatch — delegate to reload's dispatchRestart with isSelf=false.
	// (publish never invokes the self-exit choreography.)
	restartOutput, restartWarn, err := dispatchRestart(ctx, doc.Restart, false)
	warnings := []string{}
	if restartWarn != "" {
		warnings = append(warnings, restartWarn)
	}
	if err != nil {
		writeAudit(AuditEntry{
			Caller: caller, TargetSourceRepo: src, Mode: "publish", Outcome: "failure",
			PrePullSha: preSHA, PostPullSha: postSHA,
			BuildOutput:      buildOutput,
			RestartMechanism: string(doc.Restart.Mechanism),
			RestartOutput:    restartOutput,
			Warnings:         warnings,
			FailureReason:    err.Error(),
		})
		return nil, err
	}

	result := &PublishResult{
		TargetSourceRepo: src,
		Mode:             "publish",
		Branch:           targetBranch,
		WorktreeSha:      wtSHA,
		PrePublishSha:    preSHA,
		PostPublishSha:   postSHA,
		Reset:            in.Reset,
		Pushed:           pushed,
		RemoteSHA:        remoteSHA,
		BuildOutput:      buildOutput,
		RestartMechanism: string(doc.Restart.Mechanism),
		RestartOutput:    restartOutput,
		Warnings:         warnings,
	}

	writeAudit(AuditEntry{
		Caller:           caller,
		TargetSourceRepo: src,
		Mode:             "publish",
		Pulled:           false,
		PrePullSha:       preSHA,
		PostPullSha:      postSHA,
		BuildOutput:      buildOutput,
		RestartMechanism: string(doc.Restart.Mechanism),
		RestartOutput:    restartOutput,
		Warnings:         warnings,
		Outcome:          "success",
	})

	return result, nil
}
