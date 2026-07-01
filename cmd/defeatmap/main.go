// Command defeatmap is the entry point for the "Карта поражений" desktop app.
// Build it with -ldflags -H=windowsgui (see scripts/build.ps1) so no console
// window appears alongside the GUI on Windows.
package main

import (
	_ "embed"
	"log"
	"os"
	"path/filepath"

	"fyne.io/fyne/v2"
	fyneapp "fyne.io/fyne/v2/app"

	"defeatmap/internal/store"
	"defeatmap/internal/ui"
)

//go:embed icon.png
var iconPNG []byte

func main() {
	dataDir, err := store.AppDataDir()
	if err != nil {
		log.Fatalf("не удалось определить папку данных: %v", err)
	}

	st, err := store.Open(filepath.Join(dataDir, "data.json"))
	if err != nil {
		log.Fatalf("не удалось загрузить данные: %v", err)
	}

	a := fyneapp.NewWithID("com.iskander11.defeatmap")
	a.Settings().SetTheme(ui.NewTheme())

	icon := fyne.NewStaticResource("icon.png", iconPNG)
	a.SetIcon(icon)

	win := a.NewWindow("Карта поражений — Крым")
	win.SetIcon(icon)
	// Kept modest on purpose: Fyne's CenterOnScreen can occasionally
	// mis-detect monitor bounds (picks the wrong/a stale monitor) right
	// after a window is created, which would push a larger window's edge
	// off-screen. This size stays fully on-screen even if that happens.
	win.Resize(fyne.NewSize(1150, 715))
	win.SetMaster()
	win.CenterOnScreen()

	appUI := ui.NewApp(win, st, findCrimeaBundleDir())
	win.SetContent(appUI.Content())

	win.ShowAndRun()
}

// findCrimeaBundleDir locates the pre-seeded low-zoom Crimea tile overview
// that ships next to the executable (assets/tiles/crimea), falling back to
// the source-tree relative path when run via `go run` in development.
func findCrimeaBundleDir() string {
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "assets", "tiles", "crimea")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
	}
	if info, err := os.Stat("assets/tiles/crimea"); err == nil && info.IsDir() {
		return "assets/tiles/crimea"
	}
	return ""
}
