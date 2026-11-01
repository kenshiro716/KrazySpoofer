package update

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func serve(t *testing.T, status int, body string) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
	t.Cleanup(server.Close)

	previous := latestURL
	latestURL = server.URL
	t.Cleanup(func() { latestURL = previous })
}

func TestCurrentBuildRuns(t *testing.T) {
	serve(t, 200, fmt.Sprintf(`{"build":%d}`, Build))
	if err := Check(); err != nil {
		t.Fatal(err)
	}
}

func TestNewerBuildAllowed(t *testing.T) {
	serve(t, 200, fmt.Sprintf(`{"build":%d}`, Build-1))
	if err := Check(); err != nil {
		t.Fatal(err)
	}
}

func TestOutdatedBuildBlocked(t *testing.T) {
	serve(t, 200, fmt.Sprintf(`{"build":%d,"download":"https://example.com/new"}`, Build+1))

	var outdated *OutdatedError
	err := Check()
	if !errors.As(err, &outdated) || !strings.Contains(err.Error(), "Need latest") || outdated.Download != "https://example.com/new" {
		t.Fatalf("got %v", err)
	}
}

func TestFailedChecksBlock(t *testing.T) {
	for _, reply := range []struct {
		status int
		body   string
	}{{404, "404: Not Found"}, {200, "not json"}, {200, `{"other":1}`}} {
		serve(t, reply.status, reply.body)
		if err := Check(); !errors.Is(err, ErrCheckFailed) {
			t.Fatalf("%q gave %v", reply.body, err)
		}
	}
}
