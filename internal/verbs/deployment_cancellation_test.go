package verbs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anutron/iris/internal/argus"
)

// The build signals readiness outside the repository and blocks until the test
// cancels the caller. This proves cancellation happens after mutation and while
// an actual build subprocess is running, without relying on a timed sleep.
func TestDeployment_CallerCancellation(t *testing.T) {
	for _, verb := range []string{"publish-ff", "publish-reset", "reload", "reload-no-pull"} {
		for _, outcome := range []string{"success", "build-failure", "restart-failure", "build-timeout", "verify-failure"} {
			if strings.HasPrefix(verb, "publish") && outcome == "verify-failure" {
				continue
			}
			t.Run(verb+"/"+outcome, func(t *testing.T) {
				dir := t.TempDir()
				ready := filepath.Join(dir, "ready")
				release := filepath.Join(dir, "release")
				restarted := filepath.Join(dir, "restarted")
				buildExit, restartExit := 0, 0
				buildTimeout := 10
				if outcome == "build-timeout" {
					buildTimeout = 1
				}
				if outcome == "build-failure" {
					buildExit = 23
				}
				if outcome == "restart-failure" {
					restartExit = 24
				}
				body := fmt.Sprintf(`schema_version = 1
[build]
command = ["sh", "-c", "touch '%s'; while [ ! -f '%s' ]; do sleep 0.01; done; echo build-finished; exit %d"]
timeout_seconds = %d
[restart]
mechanism = "exec"
command = ["sh", "-c", "echo restart-ran; touch '%s'; exit %d"]
`, ready, release, buildExit, buildTimeout, restarted, restartExit)
				if outcome == "verify-failure" {
					body += "[verify]\ncommand = [\"sh\", \"-c\", \"echo verify-ran; exit 25\"]\n"
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				var src, expectedSHA string
				var statusClient *argus.Client
				var run func() error
				if strings.HasPrefix(verb, "publish") {
					source, wt, client := publishFixture(t, "cancelled", body)
					src, expectedSHA = source, headSHA(t, wt)
					statusClient = client
					run = func() error {
						_, err := Publish(ctx, client, PublishInput{TaskID: "cancelled", Caller: "cancelled-test", Reset: verb == "publish-reset"})
						return err
					}
				} else {
					source, client := reloadFixture(t, body, false)
					src, expectedSHA = source, headSHA(t, source)
					statusClient = client
					if verb == "reload" {
						// Make origin genuinely ahead so reload must mutate the
						// checkout before reaching the blocked build.
						g := gitRunner(t)
						g(source, "commit", "--allow-empty", "-m", "remote update")
						g(source, "push", "origin", "main")
						before := expectedSHA
						expectedSHA = headSHA(t, source)
						g(source, "reset", "--hard", before)
					}
					run = func() error {
						_, err := Reload(ctx, client, ReloadInput{Path: source, Caller: "cancelled-test", NoPull: verb == "reload-no-pull"})
						return err
					}
				}
				done := make(chan error, 1)
				go func() { done <- run() }()
				// Always unblock/join before fixture cleanup, even on assertion failure.
				t.Cleanup(func() {
					_ = os.WriteFile(release, nil, 0o600)
					select {
					case <-done:
					case <-time.After(5 * time.Second):
						t.Error("deployment failed to finish during cleanup")
					}
				})
				deadline := time.After(5 * time.Second)
				for {
					if _, err := os.Stat(ready); err == nil {
						break
					}
					select {
					case err := <-done:
						done <- err
						t.Fatalf("deployment returned before build readiness: %v", err)
					case <-deadline:
						t.Fatal("build did not become ready")
					case <-time.After(5 * time.Millisecond):
					}
				}
				if got := headSHA(t, src); got != expectedSHA {
					t.Fatalf("checkout not updated before cancellation: %s != %s", got, expectedSHA)
				}
				readyInfo, statErr := os.Stat(ready)
				if statErr != nil {
					t.Fatal(statErr)
				}
				cancel()
				if outcome != "build-timeout" {
					if err := os.WriteFile(release, nil, 0o600); err != nil {
						t.Fatal(err)
					}
				}
				var err error
				select {
				case err = <-done:
					done <- err // leave completion available to cleanup
				case <-time.After(5 * time.Second):
					t.Fatal("deployment did not finish")
				}
				if (err == nil) != (outcome == "success") {
					t.Fatalf("outcome %s: error = %v", outcome, err)
				}
				if outcome == "build-timeout" && !strings.Contains(err.Error(), "timed out") {
					t.Fatalf("expected configured build timeout: %v", err)
				}
				// Cancellation used to masquerade as a configured timeout. A
				// matching error string alone would let that bug pass this case.
				if outcome == "build-timeout" && time.Since(readyInfo.ModTime()) < 900*time.Millisecond {
					t.Fatal("build stopped at caller cancellation rather than its own timeout")
				}
				_, markerErr := os.Stat(restarted)
				wantAttempt := outcome == "success" || outcome == "restart-failure" || outcome == "verify-failure"
				if (markerErr == nil) != wantAttempt {
					t.Fatalf("restart marker error = %v, attempted = %v", markerErr, wantAttempt)
				}
				status, statusErr := Status(context.Background(), statusClient, StatusInput{Path: src})
				if statusErr != nil {
					t.Fatal(statusErr)
				}
				last := status.LastReload
				if last == nil || last.Caller != "cancelled-test" || last.PostPullSha != expectedSHA || last.Timestamp.IsZero() {
					t.Fatalf("status missing correlated terminal result: %+v", last)
				}
				wantOutcome := "failure"
				if outcome == "success" {
					wantOutcome = "success"
				}
				if last.Outcome != wantOutcome || (outcome != "success" && last.FailureReason == "") {
					t.Fatalf("unexpected persisted outcome: %+v", last)
				}
				if last.RestartAttempted != wantAttempt || last.RestartSucceeded != (outcome == "success" || outcome == "verify-failure") {
					t.Fatalf("incorrect persisted restart outcome: %+v", last)
				}
				if wantAttempt && !strings.Contains(last.RestartOutput, "restart-ran") {
					t.Fatalf("missing restart output: %+v", last)
				}
				if outcome == "verify-failure" && !strings.Contains(last.FailureReason, "verify hook failed") {
					t.Fatalf("missing verification failure: %+v", last)
				}
			})
		}
	}
}
