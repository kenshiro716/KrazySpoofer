package main

import (
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"krazyspoofer/internal/app/assets/shared/assetutils"
	"krazyspoofer/internal/app/config"
	"krazyspoofer/internal/app/request"
	"krazyspoofer/internal/app/settings"
	"krazyspoofer/internal/app/usage"
	"krazyspoofer/internal/color"
	"krazyspoofer/internal/placefile"
	"krazyspoofer/internal/roblox/games"
	"krazyspoofer/internal/roblox"
	"krazyspoofer/internal/roblox/publish"
	"krazyspoofer/internal/update"
)

//go:embed app.html
var appPage string

//go:embed icon.png
var appIcon []byte

//go:embed plugin.rbxm
var pluginFile []byte

const discordProfile = "https://discord.com/users/1194772491562926211"

// lastPluginPoll is when the Studio plugin last spoke to this executable.
var lastPluginPoll atomic.Int64

const pluginConnectedWindow = 10 * time.Second

const maxSecretSize = 16 << 10

const (
	maxPlaceFileSize = 2 << 30
	maxAppLogLines   = 5000
)

const (
	appIdle     = "idle"
	appOpened   = "opened"
	appScanning = "scanning"
	appScanned  = "scanned"
	appRunning  = "running"
	appFinished = "finished"
)

type appStatus struct {
	State      string                `json:"state"`
	FileName   string                `json:"fileName"`
	IDCount    int                   `json:"idCount"`
	Unreadable int                   `json:"unreadable"`
	Quota      *publish.AudioQuota   `json:"quota"`
	ScanDone   int                   `json:"scanDone"`
	ScanTotal  int                   `json:"scanTotal"`
	GamePasses []request.ProductInfo `json:"gamePasses"`
	Products   []request.ProductInfo `json:"products"`
	Output     string                `json:"output"`
	Error      string                `json:"error"`
	Lines      []string              `json:"lines"`
	LineCount  int                   `json:"lineCount"`

	PluginConnected bool `json:"pluginConnected"`
}

// appServer is the state behind the app window. Only one place file is open at a time.
type appServer struct {
	client *roblox.Client
	token  string

	mutex  sync.Mutex
	status appStatus
	data   []byte
	ids    []int64
	lines  []string
}

// Write collects console output so the window can show it.
func (a *appServer) Write(p []byte) (int, error) {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	for _, line := range strings.Split(strings.TrimRight(string(p), "\r\n"), "\n") {
		a.lines = append(a.lines, strings.TrimRight(line, "\r"))
	}
	if extra := len(a.lines) - maxAppLogLines; extra > 0 {
		a.lines = a.lines[extra:]
	}
	return len(p), nil
}

func (a *appServer) update(change func(s *appStatus)) {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	change(&a.status)
}

// allowed blocks other websites from driving the exe: the token is only
// readable by our own page, and the host check stops DNS rebinding.
func (a *appServer) allowed(w http.ResponseWriter, r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.Host)
	if err != nil {
		host = r.Host
	}
	if host != "localhost" && host != "127.0.0.1" {
		w.WriteHeader(http.StatusForbidden)
		return false
	}
	public := r.URL.Path == "/app" || r.URL.Path == "/app/icon.png" || r.URL.Path == "/app/plugin.rbxm"
	if !public && r.Header.Get("X-KrazySpoofer-Token") != a.token {
		w.WriteHeader(http.StatusForbidden)
		return false
	}
	return true
}

func (a *appServer) handle(pattern string, handler http.HandlerFunc) {
	http.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		if a.allowed(w, r) {
			handler(w, r)
		}
	})
}

func (a *appServer) busy() bool {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	return a.status.State == appScanning || a.status.State == appRunning
}

func writeError(w http.ResponseWriter, message string) {
	w.WriteHeader(http.StatusBadRequest)
	io.WriteString(w, message)
}

func (a *appServer) open(w http.ResponseWriter, r *http.Request) {
	if a.busy() {
		writeError(w, "A run is in progress.")
		return
	}

	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxPlaceFileSize))
	if err != nil {
		writeError(w, "Could not read that file.")
		return
	}

	ids, unreadable, err := placefile.Candidates(data)
	if err != nil {
		writeError(w, err.Error())
		return
	}
	if len(ids) == 0 {
		writeError(w, "No ids found. Is this a .rbxl or .rbxlx place file?")
		return
	}

	fileName, err := url.PathUnescape(r.Header.Get("X-File-Name"))
	if err != nil {
		fileName = "place.rbxl"
	}

	status := appStatus{
		State:      appOpened,
		FileName:   filepath.Base(fileName),
		IDCount:    len(ids),
		Unreadable: unreadable,
	}
	if quota, err := publish.GetAudioQuota(a.client); err == nil {
		status.Quota = &quota
	}

	assetutils.ClearAssetsInfoCache()

	a.mutex.Lock()
	a.status, a.data, a.ids, a.lines = status, data, ids, nil
	a.mutex.Unlock()
	w.WriteHeader(http.StatusOK)
}

func (a *appServer) scan(w http.ResponseWriter, _ *http.Request) {
	if a.busy() || a.ids == nil {
		writeError(w, "Open a place file first.")
		return
	}
	if err := update.Check(); err != nil {
		writeError(w, err.Error())
		return
	}

	a.client.SetCancelled(false)
	a.update(func(s *appStatus) { s.State = appScanning; s.ScanDone, s.ScanTotal, s.Error = 0, 0, "" })

	go func() {
		passes, products := findProducts(a.client, a.ids, func(done, total int) {
			a.update(func(s *appStatus) { s.ScanDone, s.ScanTotal = done, total })
		})
		a.update(func(s *appStatus) {
			s.State, s.GamePasses, s.Products = appScanned, passes, products
		})
	}()
	w.WriteHeader(http.StatusOK)
}

type startRequest struct {
	PlaceID      int64    `json:"placeId"`
	ShareUserID  int64    `json:"shareUserId"`
	ShareGroupID int64    `json:"shareGroupId"`
	AssetTypes   []string `json:"assetTypes"`
	GamePasses   []int64  `json:"gamePasses"`
	Products     []int64  `json:"products"`
}

func selectProducts(items []request.ProductInfo, ids []int64) []request.ProductInfo {
	wanted := make(map[int64]bool, len(ids))
	for _, id := range ids {
		wanted[id] = true
	}

	selected := make([]request.ProductInfo, 0, len(ids))
	for _, item := range items {
		if wanted[item.ID] {
			selected = append(selected, item)
		}
	}
	return selected
}

func appOutputPath(fileName string) (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}

	folder := filepath.Join(filepath.Dir(executable), "Reuploaded")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return "", err
	}
	return placeFileOutputPath(filepath.Join(folder, fileName)), nil
}

func (a *appServer) start(w http.ResponseWriter, r *http.Request) {
	if a.busy() || a.ids == nil {
		writeError(w, "Open a place file first.")
		return
	}
	if err := update.Check(); err != nil {
		writeError(w, err.Error())
		return
	}

	var body startRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.PlaceID < 0 {
		writeError(w, "That place ID is not valid.")
		return
	}

	a.mutex.Lock()
	job := &placeJob{
		PlaceID:      body.PlaceID,
		ShareUserID:  body.ShareUserID,
		ShareGroupID: body.ShareGroupID,
		AssetTypes:   body.AssetTypes,
		GamePasses:   selectProducts(a.status.GamePasses, body.GamePasses),
		Products:     selectProducts(a.status.Products, body.Products),
	}
	data, ids, fileName := a.data, a.ids, a.status.FileName
	a.status.State, a.status.Output, a.status.Error = appRunning, "", ""
	a.lines = nil
	a.mutex.Unlock()

	a.client.SetCancelled(false)
	go func() {
		output, err := a.run(job, data, ids, fileName)
		a.update(func(s *appStatus) {
			s.State, s.Output = appFinished, output
			if err != nil {
				s.Error = err.Error()
			}
		})
	}()
	w.WriteHeader(http.StatusOK)
}

func (a *appServer) run(job *placeJob, data []byte, ids []int64, fileName string) (string, error) {
	replacements, runErr := runPlaceJob(a.client, job, ids)
	if len(replacements) == 0 {
		return "", runErr
	}

	// Cancelled and failed runs still save, so finished uploads are not lost.
	output, err := appOutputPath(fileName)
	if err != nil {
		return "", err
	}
	changed, err := savePlaceFile(data, replacements, output)
	if err != nil {
		return "", err
	}

	color.Success.Println("Reuploaded ", len(replacements), " items and changed ", changed, " references.")
	return output, runErr
}

func (a *appServer) statusHandler(w http.ResponseWriter, r *http.Request) {
	var after int
	fmt.Sscan(r.URL.Query().Get("after"), &after)

	a.mutex.Lock()
	status := a.status
	if status.State == "" {
		status.State = appIdle
	}
	status.LineCount = len(a.lines)
	status.PluginConnected = time.Since(time.Unix(lastPluginPoll.Load(), 0)) < pluginConnectedWindow
	if after < len(a.lines) {
		status.Lines = append([]string(nil), a.lines[after:]...)
	}
	a.mutex.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func pluginsFolder() string {
	return filepath.Join(os.Getenv("LOCALAPPDATA"), "Roblox", "Plugins")
}

func (a *appServer) installPlugin(w http.ResponseWriter, _ *http.Request) {
	folder := pluginsFolder()
	if err := os.MkdirAll(folder, 0o755); err != nil {
		writeError(w, err.Error())
		return
	}
	if err := os.WriteFile(filepath.Join(folder, "Uploaderv3Newest.rbxm"), pluginFile, 0o644); err != nil {
		writeError(w, err.Error())
		return
	}

	message := "Plugin installed. Restart Studio to load it."
	if _, err := os.Stat(filepath.Join(folder, "Uploaderv3Newest.rbxmx")); err == nil {
		message = "Plugin installed. Remove the old Uploaderv3Newest.rbxmx from the folder, then restart Studio."
	}
	io.WriteString(w, message)
}

func (a *appServer) openPluginFolder(w http.ResponseWriter, _ *http.Request) {
	folder := pluginsFolder()
	if err := os.MkdirAll(folder, 0o755); err == nil && runtime.GOOS == "windows" {
		exec.Command("explorer", folder).Start()
	}
	w.WriteHeader(http.StatusOK)
}

func avatarURL(c *roblox.Client) string {
	address := fmt.Sprintf("https://thumbnails.roblox.com/v1/users/avatar-headshot?userIds=%d&size=150x150&format=Png", c.UserInfo.ID)
	req, err := http.NewRequest("GET", address, http.NoBody)
	if err != nil {
		return ""
	}
	resp, err := c.DoRequest(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	var body struct {
		Data []struct {
			ImageURL string `json:"imageUrl"`
		} `json:"data"`
	}
	if json.NewDecoder(resp.Body).Decode(&body) != nil || len(body.Data) == 0 {
		return ""
	}
	return body.Data[0].ImageURL
}

// account never includes the cookie or API key, only whether a key is set.
func (a *appServer) account(w http.ResponseWriter, _ *http.Request) {
	response := map[string]any{
		"id":          a.client.UserInfo.ID,
		"username":    a.client.UserInfo.Username,
		"displayName": a.client.UserInfo.DisplayName,
		"avatar":      avatarURL(a.client),
		"hasApiKey":   strings.TrimSpace(config.Get("api_key")) != "",
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (a *appServer) games(w http.ResponseWriter, _ *http.Request) {
	type game struct {
		Name    string `json:"name"`
		PlaceID int64  `json:"placeId"`
	}

	list := make([]game, 0)
	if response, err := games.UserGames(a.client, a.client.UserInfo.ID); err == nil {
		for _, item := range response.Data {
			list = append(list, game{item.Name, item.RootPlace.ID})
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

func (a *appServer) setCookie(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxSecretSize))
	if err != nil {
		writeError(w, "That cookie is too long.")
		return
	}

	previous := a.client.Cookie
	if err := a.client.SetCookie(strings.TrimSpace(string(body))); err != nil {
		a.client.Cookie = previous
		writeError(w, "That cookie did not work: "+err.Error())
		return
	}
	if err := saveCookieFile(a.client.Cookie); err != nil {
		writeError(w, "Logged in, but the cookie could not be saved: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (a *appServer) setAPIKey(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxSecretSize))
	if err != nil {
		writeError(w, "That key is too long.")
		return
	}

	key := strings.TrimSpace(string(body))
	if key == "" {
		writeError(w, "Paste an API key.")
		return
	}

	config.Set("api_key", key)
	if err := config.PersistAPIKey(); err != nil {
		writeError(w, "The key could not be saved: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (a *appServer) usage(w http.ResponseWriter, _ *http.Request) {
	response := map[string]any{"today": usage.Get()}
	if quota, err := publish.GetAudioQuota(a.client); err == nil {
		response["quota"] = quota
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (a *appServer) getSettings(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(settings.Get())
}

func (a *appServer) saveSettings(w http.ResponseWriter, r *http.Request) {
	var body settings.Settings
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, "Those settings could not be read.")
		return
	}
	if err := settings.Set(body); err != nil {
		writeError(w, err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (a *appServer) showOutput(w http.ResponseWriter, _ *http.Request) {
	a.mutex.Lock()
	output := a.status.Output
	a.mutex.Unlock()

	if output != "" && runtime.GOOS == "windows" {
		exec.Command("explorer", "/select,", output).Start()
	}
	w.WriteHeader(http.StatusOK)
}

func findChrome() string {
	for _, folder := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), os.Getenv("LOCALAPPDATA")} {
		path := filepath.Join(folder, "Google", "Chrome", "Application", "chrome.exe")
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// openInChrome falls back to the default browser if Chrome is not installed.
func openInChrome(address string) {
	if runtime.GOOS != "windows" {
		return
	}
	if chrome := findChrome(); chrome != "" && exec.Command(chrome, address).Start() == nil {
		return
	}
	exec.Command("rundll32", "url.dll,FileProtocolHandler", address).Start()
}

// openAppInBrowser is the fallback for PCs without the WebView2 runtime.
func openAppInBrowser(address string) {
	if runtime.GOOS != "windows" {
		return
	}
	if err := exec.Command("cmd", "/c", "start", "", "msedge", "--app="+address).Run(); err != nil {
		exec.Command("rundll32", "url.dll,FileProtocolHandler", address).Start()
	}
}

func registerApp(c *roblox.Client) {
	tokenBytes := make([]byte, 24)
	if _, err := rand.Read(tokenBytes); err != nil {
		color.Error.Println("App window disabled: ", err)
		return
	}

	app := &appServer{client: c, token: hex.EncodeToString(tokenBytes)}
	// The app goes first because the console writer fails once it is detached.
	color.Output = io.MultiWriter(app, color.Output)

	page := strings.Replace(appPage, "__TOKEN__", app.token, 1)
	app.handle("GET /app", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, page)
	})
	app.handle("GET /app/icon.png", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(appIcon)
	})
	app.handle("POST /app/api/discord", func(w http.ResponseWriter, _ *http.Request) {
		openInChrome(discordProfile)
		w.WriteHeader(http.StatusOK)
	})
	app.handle("GET /app/plugin.rbxm", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="Uploaderv3Newest.rbxm"`)
		w.Write(pluginFile)
	})
	app.handle("POST /app/api/account/cookie", app.setCookie)
	app.handle("POST /app/api/account/apikey", app.setAPIKey)
	app.handle("POST /app/api/open", app.open)
	app.handle("GET /app/api/account", app.account)
	app.handle("GET /app/api/usage", app.usage)
	app.handle("GET /app/api/games", app.games)
	app.handle("GET /app/api/settings", app.getSettings)
	app.handle("POST /app/api/settings", app.saveSettings)
	app.handle("POST /app/api/plugin/install", app.installPlugin)
	app.handle("POST /app/api/plugin/folder", app.openPluginFolder)
	app.handle("POST /app/api/scan", app.scan)
	app.handle("POST /app/api/start", app.start)
	app.handle("GET /app/api/status", app.statusHandler)
	app.handle("POST /app/api/show", app.showOutput)
	app.handle("POST /app/api/cancel", func(w http.ResponseWriter, _ *http.Request) {
		c.SetCancelled(true)
		w.WriteHeader(http.StatusOK)
	})

}
