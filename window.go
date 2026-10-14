// Adapted from MaveScape 0.8.0 window.go.

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// openWindow shows SituCraft. In "app" mode it starts a Chromium-family browser as a desktop
// window (no tabs or address bar) with a profile of its own, and returns a function that waits
// for that window to close. Otherwise, or when no such browser is installed, it opens the
// default browser and returns nil.
func openWindow(url, mode, dataDir string) (func(), error) {
	if mode == "app" {
		if browser := findChromium(); browser != "" {
			profile := filepath.Join(dataDir, "window-profile")
			if dataDir == "" {
				profile = filepath.Join(os.TempDir(), "situcraft-window-profile")
			}
			cmd := exec.Command(browser,
				"--app="+url,
				"--user-data-dir="+profile,
				"--no-first-run",
				"--no-default-browser-check",
				"--disable-features=Translate,MediaRouter",
				"--window-size=1680,1040",
			)
			if err := cmd.Start(); err == nil {
				return func() { cmd.Wait() }, nil
			}
		}
	}
	return nil, openBrowser(url)
}

func findChromium() string {
	var candidates []string
	switch runtime.GOOS {
	case "darwin":
		for _, root := range []string{"/Applications", filepath.Join(os.Getenv("HOME"), "Applications")} {
			candidates = append(candidates,
				filepath.Join(root, "Google Chrome.app/Contents/MacOS/Google Chrome"),
				filepath.Join(root, "Microsoft Edge.app/Contents/MacOS/Microsoft Edge"),
				filepath.Join(root, "Brave Browser.app/Contents/MacOS/Brave Browser"),
				filepath.Join(root, "Chromium.app/Contents/MacOS/Chromium"),
			)
		}
	case "windows":
		for _, root := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), os.Getenv("LocalAppData")} {
			if root == "" {
				continue
			}
			candidates = append(candidates,
				filepath.Join(root, `Google\Chrome\Application\chrome.exe`),
				filepath.Join(root, `Microsoft\Edge\Application\msedge.exe`),
				filepath.Join(root, `BraveSoftware\Brave-Browser\Application\brave.exe`),
				filepath.Join(root, `Chromium\Application\chrome.exe`),
			)
		}
	default:
		for _, name := range []string{"google-chrome", "google-chrome-stable", "microsoft-edge", "brave-browser", "chromium", "chromium-browser"} {
			if path, err := exec.LookPath(name); err == nil {
				candidates = append(candidates, path)
			}
		}
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "linux", "freebsd", "openbsd", "netbsd":
		cmd = exec.Command("xdg-open", url)
	default:
		return errors.New("unsupported platform: open " + url + " yourself")
	}
	return cmd.Start()
}
