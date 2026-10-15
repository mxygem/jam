// Package app is the game: scenes, menus and the main loop.
package app

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/audio"
	"github.com/hajimehoshi/ebiten/v2/inpututil"

	"github.com/mxygem/jam/internal/action"
	"github.com/mxygem/jam/internal/chart"
	"github.com/mxygem/jam/internal/clock"
	"github.com/mxygem/jam/internal/config"
	"github.com/mxygem/jam/internal/demo"
	"github.com/mxygem/jam/internal/input"
	"github.com/mxygem/jam/internal/library"
	"github.com/mxygem/jam/internal/playback"
	"github.com/mxygem/jam/internal/playlist"
	"github.com/mxygem/jam/internal/render"
	"github.com/mxygem/jam/internal/replay"
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

	Index  *library.Index
	Scores *replay.Scores
	Art    *artCache
	Queue  *playlist.Playlist // the playlist being built or played
	Run    *playRun           // playing through a playlist

	libMu     sync.Mutex
	library   []*chart.Song
	byDir     map[string]*chart.Song
	scanning  bool
	scanErr   string
	scanCount int

	scene     Scene
	Mouse     Mouse
	Raw       []input.RawEvent
	MenuActs  []action.Action
	Chars     []rune
	frame     int
	toast     string
	toastAt   float64
	quit      bool
	shotTaken bool
	wantShot  bool
	userShot  bool
	panics    int
	lastPanic float64

	// Screenshot mode runs on a fixed-step clock so a capture lands on
	// exactly the requested song time however slow rendering is.
	fixed    bool
	fakeNow  float64
	NearShot bool // set by scenes close to the capture time: fine steps
}

// ErrQuit ends the game loop.
var ErrQuit = fmt.Errorf("quit")

// bestEffort ignores write errors (a detached console).
type bestEffort struct{ w io.Writer }

func (b bestEffort) Write(p []byte) (int, error) {
	_, _ = b.w.Write(p)
	return len(p), nil
}

// Run starts the game.
func Run(opts Options) error {
	if err := os.MkdirAll(config.Dir(), 0o755); err == nil {
		if f, err := os.OpenFile(filepath.Join(config.Dir(), "jam.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644); err == nil {
			defer f.Close()
			// The file goes first: when jam is started by double-click,
			// Ebitengine detaches the console and every stderr write fails,
			// which would stop io.MultiWriter before it reached the file.
			log.SetOutput(io.MultiWriter(f, bestEffort{os.Stderr}))
			log.SetFlags(log.Ltime | log.Lmicroseconds)
		}
		// Fatal errors that can't be recovered (and any panic that escapes
		// the scene guard) still leave a trace even without a console.
		if cf, err := os.OpenFile(filepath.Join(config.Dir(), "crash.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
			_ = debug.SetCrashOutput(cf, debug.CrashOptions{})
			cf.Close()
		}
	}
	log.Printf("jam %s starting", Version)
	settings, first := config.Load()
	if first {
		_ = settings.Save()
	}
	b, err := input.LoadBindings(config.BindingsPath())
	if err != nil {
		log.Printf("bindings: %v", err)
	}
	a := &App{Opts: opts, Settings: settings, Bindings: b, NoAudio: os.Getenv("JAM_NOAUDIO") != ""}
	if opts.Screenshot != "" && a.NoAudio {
		a.fixed = true
		a.fakeNow = clock.Now()
		defer clock.Override(func() float64 { return a.fakeNow })()
	}
	if !a.NoAudio {
		a.Audio = audio.NewContext(playback.SampleRate)
	}
	a.SFX = playback.NewSFX(a.Audio)
	a.FFmpeg = playback.FindFFmpeg(settings.FFmpegPath)
	a.Input = input.NewManager()
	defer a.Input.Close()
	a.Menu = input.NewMenuInput(a.Bindings, a.Input.Devices())
	time.AfterFunc(time.Second, func() {
		for _, d := range a.Input.Devices() {
			log.Printf("device: %s %s", d.Src, d)
		}
	})
	log.Printf("library folders: %v, ffmpeg: %q, audio: %v", settings.LibraryPaths, a.FFmpeg, !a.NoAudio)

	a.Index = library.Open(filepath.Join(config.Dir(), "library.json.gz"))
	a.Scores = replay.LoadScores(filepath.Join(config.Dir(), "scores.json"), config.RunsDir())
	a.Art = newArtCache()
	a.Queue = &playlist.Playlist{}
	if _, err := demo.Write(config.SongsDir()); err != nil {
		log.Printf("demo song: %v", err)
	}
	a.RescanLibrary()

	ebiten.SetWindowTitle("jam")
	ebiten.SetWindowSize(1600, 900)
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	// Keep running when another window has focus: the song clock and the
	// controller threads don't stop, so the game loop shouldn't either.
	ebiten.SetRunnableOnUnfocused(true)
	ebiten.SetVsyncEnabled(settings.VSync && !a.fixed)
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
func (a *App) SetScene(s Scene) {
	log.Printf("scene: %T", s)
	a.scene = s
}

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
		start := time.Now()
		songs := chart.ScanLibrary(paths)
		byDir := make(map[string]*chart.Song, len(songs))
		for _, s := range songs {
			byDir[dirKey(s.Dir)] = s
		}
		a.libMu.Lock()
		a.library = songs
		a.byDir = byDir
		a.scanning = false
		a.libMu.Unlock()
		log.Printf("library: %d songs found in %.1fs", len(songs), time.Since(start).Seconds())
		if a.Index != nil {
			a.Index.Update(songs)
		}
	}()
}

func dirKey(dir string) string { return strings.ToLower(filepath.Clean(dir)) }

// SongByDir finds a library song by its folder.
func (a *App) SongByDir(dir string) *chart.Song {
	a.libMu.Lock()
	defer a.libMu.Unlock()
	return a.byDir[dirKey(dir)]
}

// SongInfo returns the library index entry for a song, analyzing the chart
// right away if the background indexer hasn't reached it yet.
func (a *App) SongInfo(s *chart.Song) *library.Info {
	if s == nil {
		return nil
	}
	if in := a.Index.Get(s); in != nil {
		return in
	}
	in := library.Analyze(s)
	a.Index.Put(s, in)
	return in
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

// guard turns a panic in a scene into a logged error and a trip back to the
// main menu, so one bug doesn't take the whole game down. Repeated panics
// within a few seconds end the game (the menu itself must be broken).
func (a *App) guard(where string) {
	r := recover()
	if r == nil {
		return
	}
	now := clock.Now()
	log.Printf("PANIC in %s (%T): %v\n%s", where, a.scene, r, debug.Stack())
	if now-a.lastPanic < 3 {
		a.panics++
	} else {
		a.panics = 1
	}
	a.lastPanic = now
	if a.panics >= 3 {
		log.Printf("too many errors, quitting")
		a.quit = true
		return
	}
	a.scene = NewMainMenu(a)
	a.Toast(fmt.Sprintf("Something went wrong (%v). Details are in jam.log", r))
}

// Update implements ebiten.Game.
func (a *App) Update() (err error) {
	defer a.guard("update")
	if a.quit {
		return ErrQuit
	}
	a.frame++
	if a.fixed {
		if a.NearShot {
			a.fakeNow += 1.0 / 240
		} else {
			a.fakeNow += 0.1
		}
	}
	now := clock.Now()
	a.Raw = a.Input.Poll(a.Raw[:0])
	a.MenuActs = a.Menu.Feed(a.Raw, now)
	a.Chars = ebiten.AppendInputChars(a.Chars[:0])
	a.Mouse.update()
	a.checkShotRequest()
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

// checkShotRequest saves a screenshot when F12 is pressed or when a file
// named shot.request appears in the config directory (handy for remote
// debugging without touching the game window).
func (a *App) checkShotRequest() {
	if inpututil.IsKeyJustPressed(ebiten.KeyF12) {
		a.userShot = true
	}
	if a.frame%30 == 0 {
		req := filepath.Join(config.Dir(), "shot.request")
		if _, err := os.Stat(req); err == nil {
			os.Remove(req)
			a.userShot = true
		}
	}
}

func (a *App) saveUserShot(screen *ebiten.Image) {
	a.userShot = false
	dir := filepath.Join(config.Dir(), "screenshots")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		a.Toast(err.Error())
		return
	}
	b := screen.Bounds()
	img := image.NewRGBA(b)
	screen.ReadPixels(img.Pix)
	path := filepath.Join(dir, time.Now().Format("2006-01-02 150405.000")+".png")
	f, err := os.Create(path)
	if err != nil {
		a.Toast(err.Error())
		return
	}
	_ = png.Encode(f, img)
	f.Close()
	_ = os.WriteFile(filepath.Join(dir, "latest.txt"), []byte(path), 0o644)
}

// Draw implements ebiten.Game.
func (a *App) Draw(screen *ebiten.Image) {
	defer a.guard("draw")
	if a.fixed && !a.wantShot {
		return // only the captured frame is drawn; keeps fixed-step capture fast
	}
	a.scene.Draw(screen, a)
	if a.wantShot {
		defer a.TakeScreenshot(screen)
	}
	if a.userShot {
		defer a.saveUserShot(screen)
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
