// Package update stops old copies of the executable from running.
package update

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

// Build is this executable's number. To force an update: raise this, release
// the new executable, then raise "build" in latest-app.json to the same number.
const Build = 1

var latestURL = "https://raw.githubusercontent.com/kenshiro716/KrazySpoofer/main/latest-app.json"

const (
	checkTimeout = 10 * time.Second
	checkTries   = 2
)

// ErrCheckFailed means the latest build could not be read.
var ErrCheckFailed = errors.New("couldn't check for updates. Check your internet and try again")

// OutdatedError is returned when a newer build is required.
type OutdatedError struct {
	Download string
}

func (e *OutdatedError) Error() string {
	if e.Download == "" {
		return "Need latest"
	}
	return "Need latest. Download it from " + e.Download
}

type latest struct {
	Build    *int   `json:"build"`
	Download string `json:"download"`
}

func fetch() (*latest, error) {
	client := http.Client{Timeout: checkTimeout}
	resp, err := client.Get(latestURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, errors.New(resp.Status)
	}

	var body latest
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	if body.Build == nil {
		return nil, errors.New("no build in response")
	}
	return &body, nil
}

// Check returns an error when this executable must not run.
func Check() error {
	for range checkTries {
		body, err := fetch()
		if err != nil {
			continue
		}
		if Build < *body.Build {
			return &OutdatedError{Download: body.Download}
		}
		return nil
	}
	return ErrCheckFailed
}
