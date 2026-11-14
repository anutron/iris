package verbs

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/anutron/iris/internal/config"
)

// DogfoodReadiness records exactly which configured probes passed.
type DogfoodReadiness struct {
	Status   string   `json:"status"`
	Ready    bool     `json:"ready"`
	Expected int      `json:"expected"`
	Found    int      `json:"found"`
	Evidence []string `json:"evidence"`
}

func waitDogfoodReady(ctx context.Context, b *config.DogfoodReadyBlock) (*DogfoodReadiness, error) {
	result := &DogfoodReadiness{Status: "unconfigured", Evidence: []string{}}
	if b == nil || len(b.Ports)+len(b.HealthURLs) == 0 {
		return result, nil
	}
	result.Expected = len(b.Ports) + len(b.HealthURLs)
	timeout := b.TimeoutSeconds
	if timeout == 0 {
		timeout = 120
	}
	interval := b.IntervalSeconds
	if interval == 0 {
		interval = 1
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	client := &http.Client{Timeout: time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	failed := func() (*DogfoodReadiness, error) {
		result.Status = "failed"
		return result, fmt.Errorf("dogfood readiness failed: expected %d passing checks, found %d; %s; %s", result.Expected, result.Found, strings.Join(result.Evidence, "; "), ctx.Err())
	}
	for {
		if ctx.Err() != nil {
			return failed()
		}
		previousEvidence, previousFound := result.Evidence, result.Found
		result.Found = 0
		result.Evidence = nil
		for _, port := range b.Ports {
			addr := net.JoinHostPort("localhost", strconv.Itoa(port))
			conn, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "tcp", addr)
			if err == nil {
				conn.Close()
				result.Found++
				result.Evidence = append(result.Evidence, "tcp "+addr+": open")
			} else {
				result.Evidence = append(result.Evidence, "tcp "+addr+": "+oneLine(err.Error()))
			}
		}
		for _, url := range b.HealthURLs {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err == nil {
				var resp *http.Response
				resp, err = client.Do(req)
				if err == nil {
					resp.Body.Close()
					if resp.StatusCode >= 200 && resp.StatusCode < 300 {
						result.Found++
					}
					result.Evidence = append(result.Evidence, url+": "+resp.Status)
					continue
				}
			}
			result.Evidence = append(result.Evidence, url+": "+oneLine(err.Error()))
		}
		// Do not replace the last completed pass with transport cancellation
		// noise if the overall deadline arrived during this pass.
		if ctx.Err() != nil {
			if len(previousEvidence) > 0 {
				result.Evidence, result.Found = previousEvidence, previousFound
			}
			return failed()
		}
		if result.Found == result.Expected {
			result.Ready = true
			result.Status = "ready"
			return result, nil
		}
		timer := time.NewTimer(time.Duration(interval * float64(time.Second)))
		select {
		case <-ctx.Done():
			timer.Stop()
			return failed()
		case <-timer.C:
		}
	}
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
