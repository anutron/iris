package argus

import "fmt"

// HTTPError is returned by Client when argus answers with a 4xx/5xx.
type HTTPError struct {
	Method     string
	Path       string
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("argus: %s %s: HTTP %d", e.Method, e.Path, e.StatusCode)
	}
	return fmt.Sprintf("argus: %s %s: HTTP %d: %s", e.Method, e.Path, e.StatusCode, e.Body)
}
