package verbs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/config"
)

func TestDogfoodDeploy_PinsResolvedTargetThroughReload(t *testing.T) {
	src, wt, _, sha, _ := setupDogfoodRepo(t, "deploy-pin", tomlDogfoodNone)
	var lookups atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/tasks/"):
			if lookups.Add(1) > 1 {
				http.Error(w, "task moved after resolution", 500)
				return
			}
			json.NewEncoder(w).Encode(map[string]string{"id": "task-sd", "name": "stub", "status": "in_progress", "worktree_path": wt})
		case r.URL.Path == "/api/projects/full":
			json.NewEncoder(w).Encode(map[string]any{"projects": []map[string]string{{"name": "test", "path": canonicalize(src)}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	result, err := DogfoodDeploy(context.Background(), argus.New(server.URL, "test"), "task-sd", DogfoodDeployOpts{Ref: sha})
	// Later task reads by lease liveness checks may fail non-fatally. Reload
	// itself must use the pinned target rather than resolving that task again.
	if err != nil || result == nil || result.NewSHA != sha || lookups.Load() < 1 {
		t.Fatalf("result=%+v err=%v task lookups=%d", result, err, lookups.Load())
	}
}

func TestDogfoodDeploy_ReadinessHoldsFlightAndReportsCommit(t *testing.T) {
	for _, success := range []bool{true, false} {
		t.Run(fmt.Sprint(success), func(t *testing.T) {
			probed := make(chan struct{}, 1)
			var ready atomic.Bool
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				select {
				case probed <- struct{}{}:
				default:
				}
				if !ready.Load() {
					w.WriteHeader(503)
				}
			}))
			defer srv.Close()
			body := tomlDogfoodNone + fmt.Sprintf("\n[dogfood_ready]\nhealth_urls=[%q]\ntimeout_seconds=1\ninterval_seconds=0.02\n", srv.URL)
			src, _, _, sha, client := setupDogfoodRepo(t, "deploy-flight", body)
			var result *SetDogfoodResult
			var deployErr error
			done := make(chan struct{})
			go func() {
				defer close(done)
				result, deployErr = DogfoodDeploy(context.Background(), client, "task-sd", DogfoodDeployOpts{Ref: sha})
			}()
			defer func() { ready.Store(true); <-done }()
			select {
			case <-probed:
			case <-time.After(5 * time.Second):
				t.Fatal("deploy did not probe readiness")
			}
			state, err := loadLease(canonicalize(src))
			if err != nil {
				t.Fatal(err)
			}
			if state.Holder == nil || state.Holder.InFlight == 0 {
				t.Fatalf("readiness lost lease flight: %+v", state.Holder)
			}
			// The same source mutex must stay held through readiness.
			stored, _ := repoLocks.Load(canonicalize(src))
			mutex := stored.(*sync.Mutex)
			if mutex.TryLock() {
				mutex.Unlock()
				t.Fatal("source lock released before readiness")
			}
			if success {
				ready.Store(true)
			}
			<-done
			if result == nil || result.Reload == nil || result.Reload.BuiltCommit != sha {
				t.Fatalf("result=%+v err=%v", result, deployErr)
			}
			if success {
				if deployErr != nil || !result.Reload.Readiness.Ready || result.Reload.ServedCommit != sha {
					t.Fatalf("result=%+v err=%v", result.Reload, deployErr)
				}
			} else if deployErr == nil || result.Reload.Readiness.Ready || result.Reload.ServedCommit != "" || len(result.Reload.Readiness.Evidence) != 1 {
				t.Fatalf("timeout result=%+v err=%v", result.Reload, deployErr)
			}
			state, err = loadLease(canonicalize(src))
			if err != nil {
				t.Fatal(err)
			}
			if state.Holder == nil || state.Holder.InFlight != 0 {
				t.Fatalf("flight not ended: %+v", state.Holder)
			}
			if currentBranchOrFail(t, src) != "dev" {
				t.Fatal("left dogfood after readiness")
			}
		})
	}
}

func TestDogfoodReadiness_AllChecksAndEvidence(t *testing.T) {
	var ready atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !ready.Load() {
			w.WriteHeader(503)
		}
	}))
	defer srv.Close()
	host, p, _ := net.SplitHostPort(srv.Listener.Addr().String())
	_ = host
	port, _ := strconv.Atoi(p)
	go func() { time.Sleep(75 * time.Millisecond); ready.Store(true) }()
	result, err := waitDogfoodReady(context.Background(), &config.DogfoodReadyBlock{Ports: []int{port}, HealthURLs: []string{srv.URL}, TimeoutSeconds: 2, IntervalSeconds: 0.02})
	if err != nil || !result.Ready || len(result.Evidence) != 2 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestDogfoodReadiness_TimeoutAndRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/ok", 302) }))
	defer srv.Close()
	result, err := waitDogfoodReady(context.Background(), &config.DogfoodReadyBlock{HealthURLs: []string{srv.URL}, TimeoutSeconds: 1, IntervalSeconds: 0.02})
	if err == nil || result.Ready || !strings.Contains(err.Error(), "expected 1") || !strings.Contains(err.Error(), "found 0") || !strings.Contains(err.Error(), "302") || strings.Contains(err.Error(), "\n") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestDogfoodReadiness_ClosedPort(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	result, err := waitDogfoodReady(context.Background(), &config.DogfoodReadyBlock{Ports: []int{port}, TimeoutSeconds: 1, IntervalSeconds: 0.02})
	if err == nil || result.Ready || result.Found != 0 || len(result.Evidence) != 1 || !strings.Contains(result.Evidence[0], "tcp localhost:") {
		t.Fatalf("closed port was ready: %+v %v", result, err)
	}
}

func TestDogfoodDeploy_LeavesDogfoodAndBuildsRequestedCommit(t *testing.T) {
	src, _, _, sha, client := setupDogfoodRepoFiles(t, "deploy-checkout", map[string]string{
		".iris.toml": strings.Replace(tomlDogfoodNone, `command = ["true"]`, `command = ["sh", "build.sh"]`, 1),
		"build.sh":   "git rev-parse HEAD > built-sha\ngit branch --show-current > built-branch\n",
		".gitignore": "built-sha\nbuilt-branch\n",
	})
	result, err := DogfoodDeploy(context.Background(), client, "task-sd", DogfoodDeployOpts{Ref: sha})
	if err != nil {
		t.Fatal(err)
	}
	if result.NewSHA != sha || currentBranchOrFail(t, src) != "dev" {
		t.Fatalf("result=%+v branch=%s", result, currentBranchOrFail(t, src))
	}
	if result.DogfoodLease == nil || result.DogfoodLease.Holder == nil || result.DogfoodLease.Holder.TaskID != "task-sd" {
		t.Fatalf("missing deploy lease snapshot: %+v", result.DogfoodLease)
	}
	holder := result.DogfoodLease.Holder
	if result.DogfoodLease.Timers.MaxHoldSeconds != 1800 || !holder.ExpiresAt.Equal(holder.GrantedAt.Add(1800*time.Second)) {
		t.Fatalf("deploy snapshot lost the absolute 30-minute deadline: %+v", result.DogfoodLease)
	}
	for file, want := range map[string]string{"built-sha": sha, "built-branch": "dev"} {
		b, err := os.ReadFile(filepath.Join(src, file))
		if err != nil || strings.TrimSpace(string(b)) != want {
			t.Fatalf("%s=%q err=%v", file, b, err)
		}
	}
	if result.Reload.ServedCommit != "" || result.Reload.Readiness.Status != "unconfigured" {
		t.Fatalf("unconfigured readiness claimed service: %+v", result.Reload)
	}
	// A second reload must accept the dogfood resting branch.
	if _, err := Reload(context.Background(), client, ReloadInput{TaskID: "task-sd"}); err != nil {
		t.Fatal(err)
	}
}

func TestDogfoodDeploy_SelfExitAfterLeaseCleanup(t *testing.T) {
	body := strings.Replace(tomlDogfoodNone, `mechanism = "none"`, `mechanism = "exit_code"`, 1)
	src, _, _, sha, client := setupDogfoodRepo(t, "deploy-self", body)
	bin := filepath.Join(src, "bin", "iris")
	if err := os.MkdirAll(filepath.Dir(bin), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("fake executable"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, ".git", "info", "exclude"), []byte("bin/\n"), 0600); err != nil {
		t.Fatal(err)
	}
	oldExecutable, oldExit, oldDelay := executable, exitFunc, selfExitDelay
	t.Cleanup(func() { executable = oldExecutable; exitFunc = oldExit; selfExitDelay = oldDelay })
	executable = func() (string, error) { return bin, nil }
	exited := make(chan int, 1)
	exitFunc = func(code int) { exited <- code }
	selfExitDelay = 0
	result, err := DogfoodDeploy(context.Background(), client, "task-sd", DogfoodDeployOpts{Ref: sha})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Reload.RestartPending || result.Reload.Readiness.Status != "pending" || !strings.Contains(strings.Join(result.Warnings, " "), "in-memory run handles") {
		t.Fatalf("missing self-reload caveat: %+v", result.Reload)
	}
	select {
	case code := <-exited:
		if code != 75 {
			t.Fatalf("exit %d", code)
		}
	case <-time.After(time.Second):
		t.Fatal("self exit not scheduled")
	}
	st, err := loadLease(canonicalize(src))
	if err != nil {
		t.Fatal(err)
	}
	if st.Holder == nil || st.Holder.InFlight != 0 {
		t.Fatalf("self exit interrupted lease cleanup: %+v", st.Holder)
	}
	stored, _ := repoLocks.Load(canonicalize(src))
	mutex := stored.(*sync.Mutex)
	if !mutex.TryLock() {
		t.Fatal("self exit before lock cleanup")
	}
	mutex.Unlock()
	if currentBranchOrFail(t, src) != "dev" {
		t.Fatal("self restart would land on main")
	}
}

func TestDogfoodDeploy_LeaseRefusalHasNoMutation(t *testing.T) {
	src, _, _, sha, client := setupDogfoodRepo(t, "deploy-lease", tomlDogfoodNone)
	_, err := DogfoodRelease(context.Background(), client, "task-sd")
	if err != nil {
		t.Fatal(err)
	}
	_, err = DogfoodDeploy(context.Background(), client, "task-sd", DogfoodDeployOpts{Ref: sha})
	var gate *LeaseGateError
	if !errors.As(err, &gate) {
		t.Fatalf("want lease gate, got %v", err)
	}
	if currentBranchOrFail(t, src) != "main" {
		t.Fatal("mutated checkout before lease gate")
	}
}

func TestDogfoodDeploy_ExpiredHolderMustReclaimOrOverride(t *testing.T) {
	for _, override := range []bool{false, true} {
		t.Run(fmt.Sprint(override), func(t *testing.T) {
			src, _, _, sha, client := setupDogfoodRepo(t, "deploy-expired", tomlDogfoodNone)
			ageLease(t, canonicalize(src), time.Hour)
			result, err := DogfoodDeploy(context.Background(), client, "task-sd", DogfoodDeployOpts{Ref: sha, LeaseOverride: override})
			if override {
				if err != nil || result == nil || len(result.Warnings) == 0 || !strings.HasPrefix(result.Warnings[0], "LEASE OVERRIDE:") {
					t.Fatalf("expired override: result=%+v err=%v", result, err)
				}
				if currentBranchOrFail(t, src) != "dev" {
					t.Fatal("override did not deploy dogfood")
				}
			} else {
				var gate *LeaseGateError
				if !errors.As(err, &gate) {
					t.Fatalf("expired holder deployed without reclaiming: result=%+v err=%v", result, err)
				}
				if currentBranchOrFail(t, src) != "main" {
					t.Fatal("expired lease changed host checkout")
				}
				stateDir, _ := SourceRepoStateDir(canonicalize(src))
				if m, _ := ReadManifest(stateDir); m != nil {
					t.Fatal("expired lease wrote manifest")
				}
			}
		})
	}
}

func TestDogfoodDeploy_ValidateConfigAndBuildFailure(t *testing.T) {
	for _, invalid := range []bool{true, false} {
		t.Run(fmt.Sprint(invalid), func(t *testing.T) {
			body := strings.Replace(tomlDogfoodNone, `command = ["true"]`, `command = ["false"]`, 1)
			if invalid {
				body += "\n[dogfood_ready]\nports=[0]\n"
			}
			src, _, _, sha, client := setupDogfoodRepo(t, "deploy-validation", body)
			validation, err := ValidateConfig(context.Background(), client, ValidateConfigInput{TaskID: "task-sd"})
			if err != nil {
				t.Fatal(err)
			}
			if validation.Valid == invalid {
				t.Fatalf("validation=%+v", validation)
			}
			if invalid && (len(validation.Errors) == 0 || !strings.Contains(validation.Errors[0].Field, "dogfood_ready")) {
				t.Fatalf("missing readiness validation: %+v", validation.Errors)
			}
			_, err = DogfoodDeploy(context.Background(), client, "task-sd", DogfoodDeployOpts{Ref: sha})
			if err == nil {
				t.Fatal("expected config/build failure")
			}
			if currentBranchOrFail(t, src) != "dev" {
				t.Fatal("failure restored another branch")
			}
			state, err := loadLease(canonicalize(src))
			if err != nil {
				t.Fatal(err)
			}
			if state.Holder == nil || state.Holder.InFlight != 0 {
				t.Fatalf("failure leaked flight: %+v", state.Holder)
			}
		})
	}
}

func TestDogfoodDeploy_ComposesAndAbortsConflict(t *testing.T) {
	src, wt, _, sha, client := setupDogfoodRepo(t, "deploy-compose", tomlDogfoodNone)
	g := gitRunner(t)
	g(src, "branch", "dev", sha)
	stateDir, _ := SourceRepoStateDir(canonicalize(src))
	priorManifest := sampleManifest()
	if err := WriteManifest(stateDir, priorManifest); err != nil {
		t.Fatal(err)
	}
	// Source main and worktree share a base but have independently committed config.
	result, err := DogfoodDeploy(context.Background(), client, "task-sd", DogfoodDeployOpts{Ref: "main", Compose: true})
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := ReadManifest(stateDir)
	if err != nil || manifest == nil || manifest.Base != priorManifest.Base || len(manifest.Layered) != len(priorManifest.Layered)+1 {
		t.Fatalf("lost composition manifest: %+v %v", manifest, err)
	}
	for _, ancestor := range []string{sha, "main"} {
		g(src, "merge-base", "--is-ancestor", ancestor, result.NewSHA)
	}
	if parents := strings.Fields(g(src, "rev-list", "--parents", "-n", "1", result.NewSHA)); len(parents) != 3 {
		t.Fatalf("not a --no-ff merge: %v", parents)
	}
	// Conflicting edits must leave the old dogfood tip and clean tree intact.
	os.WriteFile(filepath.Join(src, "conflict"), []byte("ours"), 0600)
	g(src, "add", "conflict")
	g(src, "commit", "-m", "ours")
	previous := headSHA(t, src)
	os.WriteFile(filepath.Join(wt, "conflict"), []byte("theirs"), 0600)
	g(wt, "add", "conflict")
	g(wt, "commit", "-m", "theirs")
	_, err = DogfoodDeploy(context.Background(), client, "task-sd", DogfoodDeployOpts{Ref: headSHA(t, wt), Compose: true})
	if err == nil || !strings.Contains(err.Error(), "compose") {
		t.Fatalf("want compose conflict, got %v", err)
	}
	if headSHA(t, src) != previous || currentBranchOrFail(t, src) != "dev" {
		t.Fatal("conflict changed dogfood")
	}
	if err := checkCleanTree(context.Background(), src); err != nil {
		t.Fatal(err)
	}
}
