package main

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"
)

func pluginConnected(t *testing.T) bool {
	t.Helper()
	recorder := httptest.NewRecorder()
	(&appServer{}).statusHandler(recorder, httptest.NewRequest("GET", "/app/api/status", nil))

	var status appStatus
	if err := json.Unmarshal(recorder.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	return status.PluginConnected
}

func TestPluginConnectedStatus(t *testing.T) {
	lastPluginPoll.Store(0)
	if pluginConnected(t) {
		t.Fatal("connected before any poll")
	}

	lastPluginPoll.Store(time.Now().Unix())
	if !pluginConnected(t) {
		t.Fatal("not connected right after a poll")
	}

	lastPluginPoll.Store(time.Now().Add(-2 * pluginConnectedWindow).Unix())
	if pluginConnected(t) {
		t.Fatal("still connected after the window passed")
	}
}
