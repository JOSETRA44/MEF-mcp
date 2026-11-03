// Package paths resolves where mef keeps its cache and local database.
package paths

import (
	"os"
	"path/filepath"
	"runtime"
)

// Home is the root of all mef state. MEF_HOME overrides it (tests and
// portable installs); otherwise it follows each OS's convention:
// %LOCALAPPDATA%\mef-mcp, ~/Library/Caches/mef-mcp, $XDG_CACHE_HOME/mef-mcp.
func Home() string {
	if h := os.Getenv("MEF_HOME"); h != "" {
		return h
	}
	if runtime.GOOS == "windows" {
		if d := os.Getenv("LOCALAPPDATA"); d != "" {
			return filepath.Join(d, "mef-mcp")
		}
	}
	if d, err := os.UserCacheDir(); err == nil {
		return filepath.Join(d, "mef-mcp")
	}
	return filepath.Join(os.TempDir(), "mef-mcp")
}

// CacheDir holds cached upstream responses; safe to delete at any time.
func CacheDir() string { return filepath.Join(Home(), "cache") }

// DBPath is the SQLite database populated by `mef sync`.
func DBPath() string {
	if p := os.Getenv("MEF_DB"); p != "" {
		return p
	}
	return filepath.Join(Home(), "mef.db")
}

// OutputDir holds generated reports (xlsx, html, md). MEF_OUTPUT overrides it.
// Reports are deliverables, not cache: mef never deletes anything here.
func OutputDir() string {
	if d := os.Getenv("MEF_OUTPUT"); d != "" {
		return d
	}
	return filepath.Join(Home(), "output")
}
