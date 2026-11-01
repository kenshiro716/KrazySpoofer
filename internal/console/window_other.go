//go:build !windows

package console

// Detach does nothing outside Windows, where there is no app window.
func Detach() {}

func attach() {}
