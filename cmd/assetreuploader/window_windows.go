//go:build windows

package main

import (
	"encoding/binary"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"time"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"

	"github.com/kenshiro716/KrazySpoofer/internal/console"
)

const (
	windowWidth  = 1280
	windowHeight = 820

	wmSetIcon      = 0x0080
	iconSmall      = 0
	iconBig        = 1
	imageIcon      = 1
	lrLoadFromFile = 0x0010
)

var (
	user32          = windows.NewLazySystemDLL("user32.dll")
	procLoadImage   = user32.NewProc("LoadImageW")
	procSendMessage = user32.NewProc("SendMessageW")
)

// newICOFile wraps a PNG in the .ico format, which allows PNG images.
func newICOFile(png []byte) []byte {
	ico := []byte{0, 0, 1, 0, 1, 0} // Reserved, type icon, one image.
	ico = append(ico, 0, 0, 0, 0)   // 256 by 256, stored as zero, no palette.
	ico = binary.LittleEndian.AppendUint16(ico, 1)
	ico = binary.LittleEndian.AppendUint16(ico, 32)
	ico = binary.LittleEndian.AppendUint32(ico, uint32(len(png)))
	ico = binary.LittleEndian.AppendUint32(ico, 22)
	return append(ico, png...)
}

func setWindowIcon(window unsafe.Pointer, folder string) {
	path := filepath.Join(folder, "icon.ico")
	if err := os.WriteFile(path, newICOFile(appIcon), 0o644); err != nil {
		return
	}
	pathPointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return
	}

	icon, _, _ := procLoadImage.Call(0, uintptr(unsafe.Pointer(pathPointer)), imageIcon, 0, 0, lrLoadFromFile)
	if icon == 0 {
		return
	}
	procSendMessage.Call(uintptr(window), wmSetIcon, iconSmall, icon)
	procSendMessage.Call(uintptr(window), wmSetIcon, iconBig, icon)
}

func waitForServer(address string) {
	for range 50 {
		connection, err := net.DialTimeout("tcp", address, 200*time.Millisecond)
		if err == nil {
			connection.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// runAppWindow shows the app and returns when it is closed. It returns false
// straight away if this PC cannot show the window.
func runAppWindow() bool {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	folder := filepath.Join(os.Getenv("LOCALAPPDATA"), "KrazySpoofer")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return false
	}

	window := webview2.NewWithOptions(webview2.WebViewOptions{
		DataPath:  filepath.Join(folder, "WebView"),
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			Title:  "KrazySpoofer",
			Width:  windowWidth,
			Height: windowHeight,
			Center: true,
		},
	})
	if window == nil {
		return false
	}
	defer window.Destroy()

	waitForServer("127.0.0.1:" + port)
	setWindowIcon(window.Window(), folder)
	window.Navigate("http://localhost:" + port + "/app")

	console.Detach()
	window.Run()
	return true
}
