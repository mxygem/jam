package app

import (
	"fmt"
	"log"
	"path/filepath"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/mxygem/jam/internal/action"
	"github.com/mxygem/jam/internal/chart"
	"github.com/mxygem/jam/internal/config"
	"github.com/mxygem/jam/internal/playlist"
	"github.com/mxygem/jam/internal/render"
)

func playlistsDir() string { return filepath.Join(config.Dir(), "playlists") }

// playRun is a playlist being played through.
type playRun struct {
	list *playlist.Playlist
	idx  int
}

// Current returns the entry being played.
func (r *playRun) Current() *playlist.Entry {
	if r == nil || r.idx < 0 || r.idx >= len(r.list.Entries) {
		return nil
	}
	return &r.list.Entries[r.idx]
}

// Next returns the entry after the current one, or nil at the end.
func (r *playRun) Next() *playlist.Entry {
	if r == nil || r.idx+1 >= len(r.list.Entries) {
		return nil
	}
	return &r.list.Entries[r.idx+1]
}

// StartPlaylist plays a.Queue from entry i, skipping songs that are gone.
func (a *App) StartPlaylist(i int) {
	a.Run = &playRun{list: a.Queue, idx: i - 1}
	a.PlaylistNext()
}

// PlaylistNext advances to the next playable entry, or ends the run.
func (a *App) PlaylistNext() {
	r := a.Run
	if r == nil {
		a.SetScene(NewPlaylistScene(a))
		return
	}
	for r.idx+1 < len(r.list.Entries) {
		r.idx++
		e := r.list.Entries[r.idx]
		song := a.SongByDir(e.Dir)
		if song == nil {
			a.Toast("Skipped " + e.Title + ": not in the library")
			log.Printf("playlist: %s missing (%s)", e.Title, e.Dir)
			continue
		}
		inst, ok := chart.ParseInstrument(e.Instrument)
		if !ok {
			inst = chart.Guitar
		}
		diff, ok := chart.ParseDifficulty(e.Difficulty)
		if !ok {
			diff = chart.Expert
		}
		a.SetScene(NewLoading(a, song, inst, diff))
		return
	}
	a.Run = nil
	a.Toast("Playlist finished")
	a.SetScene(NewPlaylistScene(a))
}

// EndPlaylist stops a run and returns to the playlist.
func (a *App) EndPlaylist() {
	a.Run = nil
	a.SetScene(NewPlaylistScene(a))
}

type plMode int

const (
	plList plMode = iota
	plEntry
	plOptions
	plLoad
	plName
)

// PlaylistScene shows and edits the working playlist.
type PlaylistScene struct {
	mode    plMode
	list    List // entries
	menu    List // entry actions / options / saved playlists
	input   TextInput
	renameF bool // naming for a rename rather than "save as"
}

// NewPlaylistScene opens the playlist screen.
func NewPlaylistScene(a *App) *PlaylistScene {
	p := &PlaylistScene{}
	p.rebuild(a)
	return p
}

func (p *PlaylistScene) rebuild(a *App) {
	sel := p.list.Sel
	var items []Item
	for i, e := range a.Queue.Entries {
		i, e := i, e
		label := e.Title
		if a.SongByDir(e.Dir) == nil {
			label += "  (missing)"
		}
		items = append(items, Item{Label: label, Value: func() string { return partShort(e) }, Activate: func() { p.openEntry(a, i) }})
	}
	p.list = List{Items: items, Visible: 12, Sel: max(0, min(sel, len(items)-1))}
}

func partShort(e playlist.Entry) string {
	inst, _ := chart.ParseInstrument(e.Instrument)
	diff, _ := chart.ParseDifficulty(e.Difficulty)
	return diff.Title() + " " + inst.Title()
}

func (p *PlaylistScene) openEntry(a *App, i int) {
	e := &a.Queue.Entries[i]
	song := a.SongByDir(e.Dir)
	cycle := func(d int) {
		if song == nil {
			return
		}
		in := a.SongInfo(song)
		if in == nil || len(in.Parts) == 0 {
			return
		}
		cur, _ := chart.ParseInstrument(e.Instrument)
		idx := 0
		for k, pt := range in.Parts {
			if pt.Inst == cur {
				idx = k
			}
		}
		next := in.Parts[(idx+d+len(in.Parts))%len(in.Parts)]
		e.Instrument = next.Inst.String()
		diff, _ := chart.ParseDifficulty(e.Difficulty)
		if !hasDiff(next.Diffs, diff) {
			e.Difficulty = next.Diffs[0].String()
		}
	}
	cycleDiff := func(d int) {
		if song == nil {
			return
		}
		in := a.SongInfo(song)
		inst, _ := chart.ParseInstrument(e.Instrument)
		pt := in.Part(inst)
		if pt == nil {
			return
		}
		cur, _ := chart.ParseDifficulty(e.Difficulty)
		idx := 0
		for k, x := range pt.Diffs {
			if x == cur {
				idx = k
			}
		}
		e.Difficulty = pt.Diffs[(idx-d+len(pt.Diffs))%len(pt.Diffs)].String()
	}
	p.menu = List{Visible: 7, Items: []Item{
		{Label: "Play from here", Activate: func() { a.StartPlaylist(i) }},
		{Label: "Instrument", Value: func() string { inst, _ := chart.ParseInstrument(e.Instrument); return inst.Title() }, Adjust: cycle},
		{Label: "Difficulty", Value: func() string { d, _ := chart.ParseDifficulty(e.Difficulty); return d.Title() }, Adjust: cycleDiff},
		{Label: "Move up", Activate: func() { p.list.Sel = a.Queue.Move(i, -1); p.mode = plList; p.rebuild(a) }},
		{Label: "Move down", Activate: func() { p.list.Sel = a.Queue.Move(i, 1); p.mode = plList; p.rebuild(a) }},
		{Label: "Remove", Activate: func() { a.Queue.Remove(i); p.mode = plList; p.rebuild(a) }},
		{Label: "Back", Activate: func() { p.mode = plList; p.rebuild(a) }},
	}}
	p.mode = plEntry
}

func (p *PlaylistScene) openOptions(a *App) {
	q := a.Queue
	items := []Item{}
	if len(q.Entries) > 0 {
		items = append(items, Item{Label: "Play all", Activate: func() { a.StartPlaylist(0) }})
	}
	if q.Name != "" {
		items = append(items, Item{Label: "Save", Hint: "Save changes to \"" + q.Name + "\"", Activate: func() { p.save(a, q.Name) }})
	}
	items = append(items,
		Item{Label: "Save as...", Activate: func() { p.askName(q.Name, false) }},
		Item{Label: "Load a saved playlist...", Activate: func() { p.openLoad(a) }},
	)
	if q.Name != "" {
		items = append(items, Item{Label: "Rename...", Activate: func() { p.askName(q.Name, true) }})
	}
	items = append(items,
		Item{Label: "New empty playlist", Activate: func() { a.Queue = &playlist.Playlist{}; p.mode = plList; p.rebuild(a) }},
		Item{Label: "Add songs", Hint: "Back to the song list: pick a song, then \"Add to playlist\"", Activate: func() { a.SetScene(NewSongSelect(a)) }},
		Item{Label: "Close", Activate: func() { p.mode = plList }},
	)
	p.menu = List{Visible: 8, Items: items}
	p.mode = plOptions
}

func (p *PlaylistScene) askName(cur string, rename bool) {
	p.input = TextInput{Value: cur, Placeholder: "playlist name", Max: 60}
	p.renameF = rename
	p.mode = plName
}

func (p *PlaylistScene) save(a *App, name string) {
	var err error
	if p.renameF {
		err = a.Queue.Rename(playlistsDir(), name)
	} else {
		a.Queue.Name = name
		err = a.Queue.Save(playlistsDir())
	}
	if err != nil {
		a.Toast("Couldn't save the playlist: " + err.Error())
	} else {
		a.Toast("Saved playlist \"" + name + "\"")
	}
	p.renameF = false
	p.mode = plList
}

func (p *PlaylistScene) openLoad(a *App) {
	saved := playlist.Saved(playlistsDir())
	var items []Item
	for _, pl := range saved {
		pl := pl
		items = append(items, Item{Label: pl.Name, Value: func() string { return plural(len(pl.Entries), "song") },
			Activate: func() {
				a.Queue = pl
				p.mode = plList
				p.rebuild(a)
				a.Toast("Loaded \"" + pl.Name + "\"")
			}})
	}
	if len(items) == 0 {
		items = append(items, Item{Label: "No saved playlists yet", Disabled: true})
	}
	items = append(items, Item{Label: "Back", Activate: func() { p.mode = plList }})
	p.menu = List{Visible: 9, Items: items}
	p.mode = plLoad
}

func (p *PlaylistScene) Update(a *App) error {
	switch p.mode {
	case plName:
		done, cancel := p.input.Update(a)
		if cancel {
			p.mode = plList
		} else if done && p.input.Value != "" {
			p.save(a, p.input.Value)
		}
		return nil
	case plEntry, plOptions, plLoad:
		if p.menu.Update(a) {
			return nil
		}
		if a.Pressed(action.MenuBack) || a.Mouse.Right {
			p.mode = plList
			p.rebuild(a)
		}
		return nil
	}
	if p.list.Update(a) {
		return nil
	}
	if a.Pressed(action.MenuOptions) || len(a.Queue.Entries) == 0 && a.Pressed(action.MenuConfirm) {
		p.openOptions(a)
	}
	if a.Pressed(action.MenuBack) {
		if a.playlistFromMenu {
			a.SetScene(NewMainMenu(a))
		} else {
			a.SetScene(NewSongSelect(a))
		}
	}
	return nil
}

func (p *PlaylistScene) Draw(dst *ebiten.Image, a *App) {
	MenuBackground(dst)
	name := a.Queue.Name
	if name == "" {
		name = "Unsaved playlist"
	}
	Header(dst, name, fmt.Sprintf("%s   ·   Tab / yellow: play, save, load   ·   Enter / green: song options", plural(len(a.Queue.Entries), "song")))
	Panel(dst, 96, 216, 1728, 790)
	if len(a.Queue.Entries) == 0 {
		render.Text(dst, "This playlist is empty.", render.UIBold, 34, 140, 260, th.Text, render.Left)
		render.Text(dst, "In the song list, pick a song and choose \"Add to playlist\" (or Tab / yellow > Add to playlist).", render.UI, 28, 140, 320, th.TextDim, render.Left)
		render.Text(dst, "Press Tab / yellow to load a saved playlist.", render.UI, 28, 140, 370, th.TextDim, render.Left)
	} else {
		for i := p.list.Top; i < len(p.list.Items) && i < p.list.Top+p.list.Visible; i++ {
			render.Text(dst, fmt.Sprintf("%d", i+1), render.Mono, 24, 120, 250+float64(i-p.list.Top)*62, th.TextDim, render.Left)
		}
		p.list.Draw(dst, 180, 244, 1600, 62)
	}
	if a.Run != nil {
		render.Text(dst, "Playing...", render.Cond, 24, 1800, 160, th.Accent, render.Right)
	}
	switch p.mode {
	case plEntry, plOptions, plLoad:
		render.FillRect(dst, 0, 0, render.ScreenW, render.ScreenH, render.RGBA(0, 0, 0, 0xB8))
		Panel(dst, 520, 200, 880, 720)
		title := map[plMode]string{plEntry: "Song", plOptions: "Playlist", plLoad: "Saved playlists"}[p.mode]
		render.Text(dst, title, render.Display, 52, 560, 220, th.Text, render.Left)
		p.menu.Draw(dst, 580, 320, 760, 66)
		render.Text(dst, p.menu.Hint(), render.UI, 24, 560, 870, th.TextDim, render.Left)
	case plName:
		render.FillRect(dst, 0, 0, render.ScreenW, render.ScreenH, render.RGBA(0, 0, 0, 0xB8))
		Panel(dst, 520, 400, 880, 240)
		render.Text(dst, "Name this playlist", render.UIBold, 34, 560, 420, th.Text, render.Left)
		p.input.Draw(dst, 560, 490, 800)
		render.Text(dst, "Enter to save   Esc to cancel", render.Cond, 22, 560, 580, th.TextDim, render.Left)
	}
	Footer(dst, "Enter / green song options   Tab / yellow playlist options   Esc / red back to songs")
}
