// Package app is the game: scenes, menus and the main loop.
package app

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
	"sync"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/audio"
	"github.com/hajimehoshi/ebiten/v2/inpututil"

	"github.com/mxygem/jam/internal/action"
	"github.com/mxygem/jam/internal/chart"
	"github.com/mxygem/jam/internal/clock"
	"github.com/mxygem/jam/internal/config"
	"github.com/mxygem/jam/internal/demo"
	"github.com/mxygem/jam/internal/input"
	"github.com/mxygem/jam/internal/playback"
	"github.com/mxygem/jam/internal/render"
)

// Version of the app.
const Version = "0.1.0"

// Options are command line options for the game.
type Options struct {
	Song       string  // song folder to start straight into
	Instrument string  //
	Difficulty string  //
	Autoplay   bool    // bot plays perfectly (testing/demo)
	Screenshot string  // write a PNG of the screen and exit
	ShotAt     float64 // song time for the screenshot
	ShotScene  string  // "play" (default), "menu", "results", "wizard", "select"
	Windowed   bool
}

// Scene is one screen of the game.
type Scene interface {
	Update(a *App) error
	Draw(dst *ebiten.Image, a *App)
}

// App holds everything shared between scenes.
type App struct {
	Opts     Options
	Settings *config.Settings
	Bindings *input.Bindings
	Input    *input.Manager
	Menu     *input.MenuInput
	Audio    *audio.Context
	SFX      *playback.SFX
	FFmpeg   string
	NoAudio  bool // JAM_NOAUDIO=1: no sound device (headless tests)

	libMu      sync.Mutex
	library    []*chart.Song
	scanning   bool
	scanErr    string
	scanCount  int

	scene     Scene
	Raw       []input.RawEvent
	MenuActs  []action.Action
	Chars     []rune
	frame     int
	toast     string
	toastAt   float64
	quit      bool
	shotTaken bool
	wantShot  bool
}

// ErrQuit ends the game loop.
var ErrQuit = fmt.Errorf("quit")

// Run starts the game.
func Run(opts Options) error {
	settings, first := config.Load()
	if first {
		_ = settings.Save()
	}
	b, err := input.LoadBindings(config.BindingsPath())
	if err != nil {
		log.Printf("bindings: %v", err)
	}
	a := &App{Opts: opts, Settings: settings, Bindings: b, NoAudio: os.Getenv("JAM_NOAUDIO") != ""}
	if !a.NoAudio {
		a.Audio = audio.NewContext(playback.SampleRate)
	}
	a.SFX = playback.NewSFX(a.Audio)
	a.FFmpeg = playback.FindFFmpeg(settings.FFmpegPath)
	a.Input = input.NewManager()
	defer a.Input.Close()
	a.Menu = input.NewMenuInput(a.Bindings, a.Input.Devices())

	if _, err := demo.Write(config.SongsDir()); err != nil {
		log.Printf("demo song: %v", err)
	}
	a.RescanLibrary()

	ebiten.SetWindowTitle("jam")
	ebiten.SetWindowSize(1600, 900)
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	ebiten.SetVsyncEnabled(settings.VSync)
	ebiten.SetTPS(ebiten.SyncWithFPS)
	if settings.Fullscreen && !opts.Windowed {
		ebiten.SetFullscreen(true)
	}
	a.scene = a.initialScene()
	err = ebiten.RunGameWithOptions(a, &ebiten.RunGameOptions{InitUnfocused: false})
	if err == ErrQuit {
		return nil
	}
	return err
}

func (a *App) initialScene() Scene {
	if a.Opts.Song != "" {
		s, err := chart.LoadSongFolder(a.Opts.Song)
		if err != nil {
			a.Toast("Could not open " + a.Opts.Song + ": " + err.Error())
			return NewMainMenu(a)
		}
		inst, ok := chart.ParseInstrument(a.Opts.Instrument)
		if !ok {
			inst = chart.Guitar
		}
		diff, ok := chart.ParseDifficulty(a.Opts.Difficulty)
		if !ok {
			diff = chart.Expert
		}
		return NewLoading(a, s, inst, diff)
	}
	switch a.Opts.ShotScene {
	case "wizard":
		return NewWizard(a, input.FamilyGuitar)
	case "select":
		return NewSongSelect(a)
	}
	return NewMainMenu(a)
}

// SetScene switches screens.
func (a *App) SetScene(s Scene) { a.scene = s }

// Toast shows a short notice at the bottom of the screen.
func (a *App) Toast(msg string) {
	a.toast = msg
	a.toastAt = clock.Now()
	log.Print(msg)
}

// Library returns the scanned songs.
func (a *App) Library() ([]*chart.Song, bool) {
	a.libMu.Lock()
	defer a.libMu.Unlock()
	return a.library, a.scanning
}

// RescanLibrary scans the library folders in the background.
func (a *App) RescanLibrary() {
	a.libMu.Lock()
	if a.scanning {
		a.libMu.Unlock()
		return
	}
	a.scanning = true
	paths := append([]string(nil), a.Settings.LibraryPaths...)
	a.libMu.Unlock()
	if !contains(paths, config.SongsDir()) {
		paths = append(paths, config.SongsDir())
	}
	go func() {
		songs := chart.ScanLibrary(paths)
		a.libMu.Lock()
		a.library = songs
		a.scanning = false
		a.libMu.Unlock()
	}()
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// SaveBindings persists bindings and refreshes menu navigation.
func (a *App) SaveBindings() error {
	a.Menu = input.NewMenuInput(a.Bindings, a.Input.Devices())
	return a.Bindings.Save(config.BindingsPath())
}

// Update implements ebiten.Game.
func (a *App) Update() error {
	if a.quit {
		return ErrQuit
	}
	a.frame++
	now := clock.Now()
	a.Raw = a.Input.Poll(a.Raw[:0])
	a.MenuActs = a.Menu.Feed(a.Raw, now)
	a.Chars = ebiten.AppendInputChars(a.Chars[:0])
	if inpututil.IsKeyJustPressed(ebiten.KeyF11) || (inpututil.IsKeyJustPressed(ebiten.KeyEnter) && ebiten.IsKeyPressed(ebiten.KeyAlt)) {
		fs := !ebiten.IsFullscreen()
		ebiten.SetFullscreen(fs)
		a.Settings.Fullscreen = fs
		_ = a.Settings.Save()
	}
	return a.scene.Update(a)
}

// Pressed reports whether a menu action was pressed this frame.
func (a *App) Pressed(act action.Action) bool {
	for _, m := range a.MenuActs {
		if m == act {
			return true
		}
	}
	return false
}

// Quit ends the game after this frame.
func (a *App) Quit() { a.quit = true }

// TakeScreenshotNext captures the next drawn frame (when --screenshot is set).
func (a *App) TakeScreenshotNext() {
	if a.Opts.Screenshot != "" {
		a.wantShot = true
	}
}

// Draw implements ebiten.Game.
func (a *App) Draw(screen *ebiten.Image) {
	a.scene.Draw(screen, a)
	if a.wantShot {
		defer a.TakeScreenshot(screen)
	}
	if a.toast != "" {
		dt := clock.Now() - a.toastAt
		if dt < 4 {
			alpha := 1.0
			if dt > 3.4 {
				alpha = (4 - dt) / 0.6
			}
			w, _ := render.Measure(a.toast, render.UI, 26)
			x := (render.ScreenW - w) / 2
			render.RoundRect(screen, x-24, render.ScreenH-96, w+48, 54, 14, render.WithAlpha(color.RGBA{0x20, 0x24, 0x34, 0xF0}, alpha))
			render.Text(screen, a.toast, render.UI, 26, render.ScreenW/2, render.ScreenH-86, render.WithAlpha(render.DefaultTheme.Text, alpha), render.Center)
		} else {
			a.toast = ""
		}
	}
}

// Layout implements ebiten.Game: a fixed 1080p canvas scaled to the window.
func (a *App) Layout(w, h int) (int, int) { return render.ScreenW, render.ScreenH }

// TakeScreenshot writes the screen to a PNG (used for automated checks).
func (a *App) TakeScreenshot(screen *ebiten.Image) {
	if a.Opts.Screenshot == "" || a.shotTaken {
		return
	}
	a.shotTaken = true
	b := screen.Bounds()
	img := image.NewRGBA(b)
	screen.ReadPixels(img.Pix)
	f, err := os.Create(a.Opts.Screenshot)
	if err != nil {
		log.Print(err)
		a.Quit()
		return
	}
	_ = png.Encode(f, img)
	f.Close()
	a.Quit()
}
