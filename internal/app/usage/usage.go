// Package usage counts what was reuploaded today.
package usage

import (
	"encoding/json"
	"os"
	"sync"
	"time"

	_ "github.com/kenshiro716/KrazySpoofer/internal/app/config" // Moves to the exe folder first.
)

const usageFile = "krazyspoofer-usage.json"

// Today maps an asset type to how many were reuploaded today.
type Today map[string]int

type stored struct {
	Date   string `json:"date"`
	Counts Today  `json:"counts"`
}

var (
	mutex sync.Mutex
	state stored
)

func init() {
	if data, err := os.ReadFile(usageFile); err == nil {
		_ = json.Unmarshal(data, &state)
	}
}

func rollOver() {
	date := time.Now().Format("2006-01-02")
	if state.Date != date || state.Counts == nil {
		state = stored{Date: date, Counts: Today{}}
	}
}

// Add records reuploads of one asset type.
func Add(assetType string, count int) {
	if count == 0 {
		return
	}

	mutex.Lock()
	defer mutex.Unlock()

	rollOver()
	state.Counts[assetType] += count
	if data, err := json.Marshal(state); err == nil {
		_ = os.WriteFile(usageFile, data, 0o644)
	}
}

// Get returns today's counts.
func Get() Today {
	mutex.Lock()
	defer mutex.Unlock()

	rollOver()
	counts := make(Today, len(state.Counts))
	for assetType, count := range state.Counts {
		counts[assetType] = count
	}
	return counts
}
