//go:build windows

package cli

import "golang.org/x/sys/windows"

// cpUTF8 is the Windows code page identifier for UTF-8.
const cpUTF8 = 65001

// configureConsole switches the Windows console to UTF-8 (code page 65001),
// so "Perú" and "Ñuñoa" print correctly instead of as cp850 mojibake
// (ARSENAL-SPEC §9). Redirected output is unaffected: Go always writes UTF-8.
func configureConsole() {
	_ = windows.SetConsoleOutputCP(cpUTF8)
	_ = windows.SetConsoleCP(cpUTF8)
}
