// Package settings holds the options set in the app's Configuration page.
package settings

import (
	"encoding/json"
	"os"
	"sync"

	_ "krazyspoofer/internal/app/config" // Moves to the exe folder first.
)

const settingsFile = "krazyspoofer-settings.json"

// AssetDescription is set on every animation and mesh this tool uploads.
const AssetDescription = "Made By Ranji"

type Settings struct {
	SkipOwned          bool    `json:"skipOwned"`
	SkipPublic         bool    `json:"skipPublic"`
	SkipUserIDs        []int64 `json:"skipUserIds"`
	SkipGroupIDs       []int64 `json:"skipGroupIds"`
	ProductDescription string  `json:"productDescription"`
}

var (
	mutex   sync.RWMutex
	current = Settings{
		SkipOwned:  true,
		SkipPublic: true,
	}
)

func init() {
	data, err := os.ReadFile(settingsFile)
	if err != nil {
		return
	}
	_ = json.Unmarshal(data, &current)
}

func Get() Settings {
	mutex.RLock()
	defer mutex.RUnlock()
	return current
}

// Set saves new settings.
func Set(s Settings) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(settingsFile, data, 0o644); err != nil {
		return err
	}

	mutex.Lock()
	current = s
	mutex.Unlock()
	return nil
}
