//go:build windows

package console

import (
	"os"
	"sync/atomic"

	"golang.org/x/sys/windows"
)

var (
	kernel32         = windows.NewLazySystemDLL("kernel32.dll")
	procFreeConsole  = kernel32.NewProc("FreeConsole")
	procAllocConsole = kernel32.NewProc("AllocConsole")

	detached atomic.Bool
)

// Detach closes the console once the app window has taken over.
func Detach() {
	procFreeConsole.Call()
	detached.Store(true)
}

// attach brings a console back so the user can answer a prompt.
func attach() {
	if !detached.CompareAndSwap(true, false) {
		return
	}

	procAllocConsole.Call()
	if input, err := os.OpenFile("CONIN$", os.O_RDWR, 0); err == nil {
		os.Stdin = input
	}
	if output, err := os.OpenFile("CONOUT$", os.O_RDWR, 0); err == nil {
		os.Stdout = output
	}
}
