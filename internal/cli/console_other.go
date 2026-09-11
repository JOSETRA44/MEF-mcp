//go:build !windows

package cli

// configureConsole is a no-op off Windows: POSIX terminals are UTF-8 already.
func configureConsole() {}
