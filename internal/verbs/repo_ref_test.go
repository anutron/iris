package verbs

import (
	"context"
	"testing"
)

func TestParseGitHubRepoName(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"git@github.com:acme/video-gen.git":       "acme/video-gen",
		"git@github.com:acme/video-gen":           "acme/video-gen",
		"ssh://git@github.com/acme/video-gen.git": "acme/video-gen",
		"https://github.com/acme/video-gen.git":   "acme/video-gen",
		"https://github.com/acme/video-gen":       "acme/video-gen",
		"https://github.com/acme/video-gen/":      "acme/video-gen",
		"https://user@github.com/acme/my.repo":    "acme/my.repo",
		"/tmp/remotes/origin.git":                 "",
		"file:///tmp/remotes/acme/origin.git":     "",
		"../origin.git":                           "",
		"https://gitlab.com/group/sub/repo.git":   "",
		"":                                        "",
	}
	for in, want := range cases {
		if got := ParseGitHubRepoName(in); got != want {
			t.Errorf("ParseGitHubRepoName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRepoFromPRURL(t *testing.T) {
	t.Parallel()
	if got := repoFromPRURL("https://github.com/acme/video-gen/pull/11"); got != "acme/video-gen" {
		t.Fatalf("got %q", got)
	}
	if got := repoFromPRURL("not a url"); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestValidExpectedRepo(t *testing.T) {
	t.Parallel()
	for _, ok := range []string{"acme/video-gen", "ACME/x.y_z"} {
		if !ValidExpectedRepo(ok) {
			t.Errorf("%q should be valid", ok)
		}
	}
	for _, bad := range []string{"", "video-gen", "acme/", "/x", "a/b/c", "https://github.com/a/b"} {
		if ValidExpectedRepo(bad) {
			t.Errorf("%q should be invalid", bad)
		}
	}
}

func TestRepoRecorder_FirstWins(t *testing.T) {
	t.Parallel()
	ctx, rec := WithRepoRecorder(context.Background())
	if rec.Ref() != nil {
		t.Fatal("fresh recorder should be empty")
	}
	RecordRepo(ctx, &RepoRef{Name: "a/one", Path: "/one"})
	RecordRepo(ctx, &RepoRef{Name: "b/two", Path: "/two"})
	if got := rec.Ref(); got == nil || got.Name != "a/one" {
		t.Fatalf("got %+v, want a/one", got)
	}
	// No recorder on ctx: no panic, nothing recorded.
	RecordRepo(context.Background(), &RepoRef{Path: "/x"})
}

func TestResolve_RecordsRepoWithOriginName(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "record-origin")
	client := stubArgus(t, src, wt)
	if _, err := runGit(context.Background(), src, "remote", "set-url", "origin", "git@github.com:acme/video-gen.git"); err != nil {
		t.Fatalf("set-url: %v", err)
	}

	ctx, rec := WithRepoRecorder(context.Background())
	resolved, err := Resolve(ctx, client, "task-record")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	got := rec.Ref()
	if got == nil {
		t.Fatal("expected Resolve to record the repo")
	}
	if got.Name != "acme/video-gen" || got.Path != resolved.SourceRepo {
		t.Fatalf("got %+v, want acme/video-gen at %s", got, resolved.SourceRepo)
	}
}

func TestResolve_LocalOriginRecordsPathOnly(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "record-local")
	client := stubArgus(t, src, wt)

	ctx, rec := WithRepoRecorder(context.Background())
	resolved, err := Resolve(ctx, client, "task-local")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	got := rec.Ref()
	if got == nil || got.Name != "" || got.Path != resolved.SourceRepo {
		t.Fatalf("got %+v, want path-only %s", got, resolved.SourceRepo)
	}
}

func TestResolvePath_RecordsRepo(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "record-path")
	client := stubArgus(t, src, wt)

	ctx, rec := WithRepoRecorder(context.Background())
	resolved, err := ResolvePath(ctx, client, src)
	if err != nil {
		t.Fatalf("resolve path: %v", err)
	}
	if got := rec.Ref(); got == nil || got.Path != resolved.SourceRepo {
		t.Fatalf("got %+v, want %s", got, resolved.SourceRepo)
	}
}
