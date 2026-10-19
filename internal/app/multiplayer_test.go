package app

import (
	"math/rand"
	"os"
	"testing"

	"github.com/mxygem/jam/internal/chart"
	"github.com/mxygem/jam/internal/config"
	"github.com/mxygem/jam/internal/input"
	"github.com/mxygem/jam/internal/replay"
)

// pad is an XInput button event on controller dev.
func pad(dev, bit int, down bool) input.RawEvent {
	e := xbtn(bit, down)
	e.Device = dev
	return e
}

// tapOn presses and releases a button on controller dev.
func (h *harness) tapOn(dev, bit int) {
	h.frame(pad(dev, bit, true))
	h.idle(0.08)
	h.frame(pad(dev, bit, false))
	h.idle(0.05)
}

// mashOn is mashGuitar for several controllers at once.
func mashOn(h *harness, rng *rand.Rand, devs ...int) func() []input.RawEvent {
	var fns []func() []input.RawEvent
	for _, d := range devs {
		d := d
		f := mashGuitar(h, rng)
		fns = append(fns, func() []input.RawEvent {
			evs := f()
			for i := range evs {
				evs[i].Device = d
			}
			return evs
		})
	}
	return func() []input.RawEvent {
		var out []input.RawEvent
		for _, f := range fns {
			out = append(out, f()...)
		}
		return out
	}
}

// waitLoad waits for a band song to load and start.
func (h *harness) waitBandPlay() *BandPlay {
	h.t.Helper()
	h.until("band play", 20, sceneIs[*BandPlay](h), nil)
	return h.a.scene.(*BandPlay)
}

func TestProfilesKeepTheirOwnSettings(t *testing.T) {
	h := newHarness(t)
	a := h.a
	if len(a.Profiles.All()) != 1 || !a.Active.Legacy {
		t.Fatalf("expected one legacy profile, got %d", len(a.Profiles.All()))
	}
	first := a.Active
	a.Settings.AudioOffsetMs = 55
	_ = a.Settings.Save()
	p2 := a.NewProfile("Sam")
	if p2.Name != "Sam" || p2.Color == first.Color {
		t.Fatalf("new profile %q color %s", p2.Name, p2.Color)
	}
	if p2.Player.AudioOffsetMs != 55 {
		t.Fatal("a new profile should start from the active profile's settings")
	}
	a.UseProfile(p2)
	a.Settings.AudioOffsetMs = 12
	a.Settings.Engine.NoFail = false
	_ = a.Settings.Save()
	a.UseProfile(first)
	if a.Settings.AudioOffsetMs != 55 || !a.Settings.Engine.NoFail {
		t.Fatalf("switching back should restore the first profile's settings: offset %v nofail %v", a.Settings.AudioOffsetMs, a.Settings.Engine.NoFail)
	}
	if s := a.SettingsFor(p2); s.AudioOffsetMs != 12 || s.Engine.NoFail {
		t.Fatal("SettingsFor should give the other profile's own settings")
	}
	// reload from disk
	a2 := &App{Settings: config.Default()}
	a2.Settings.ActiveProfile = p2.ID
	a2.initProfiles()
	if a2.Active.ID != p2.ID || a2.Settings.AudioOffsetMs != 12 {
		t.Fatalf("reloaded: active %s offset %v", a2.Active.Name, a2.Settings.AudioOffsetMs)
	}
	if n := a2.Profiles.UniqueName("sam", nil); n != "sam 2" {
		t.Fatalf("unique name %q", n)
	}
}

func TestLobbyToBandResults(t *testing.T) {
	h := newHarness(t)
	withDemoLibrary(h)
	a := h.a
	a.scene = NewMainMenu(a)
	h.idle(0.1)
	h.tap(bitDPadDown) // Multiplayer
	h.tap(bitA)
	if !sceneIs[*Lobby](h)() {
		t.Fatalf("scene %T, want the lobby", a.scene)
	}
	l := a.scene.(*Lobby)
	h.tapOn(0, bitA) // guitar 1 joins
	h.tapOn(1, bitA) // guitar 2 joins
	if n := len(l.band.Members); n != 2 {
		t.Fatalf("%d players joined, want 2", n)
	}
	m1, m2 := l.band.Members[0], l.band.Members[1]
	if m1.prof != a.Active || m2.prof == m1.prof {
		t.Fatal("players should get different profiles, the first the active one")
	}
	// guitar 2 switches to bass (orange is right) and readies up
	h.tapOn(1, bitDPadDown)
	h.tapOn(1, bitLB)
	if m2.inst != chart.Bass {
		t.Fatalf("player 2 instrument %s, want bass", m2.inst.Title())
	}
	h.tapOn(1, bitDPadDown)
	h.tapOn(1, bitDPadDown)
	h.tapOn(1, bitA)
	if !l.cards[m2].ready {
		t.Fatal("player 2 should be ready")
	}
	// guitar 1 (the host) goes down past Mode and Layout to Ready
	for i := 0; i < 5; i++ {
		h.tapOn(0, bitDPadDown)
	}
	h.tapOn(0, bitA)
	h.until("song list", 3, sceneIs[*SongSelect](h), nil)
	h.tap(bitA) // the band picker
	h.tap(bitA) // Play
	p := h.waitBandPlay()
	if len(p.seats) != 2 || p.seats[1].t.Key.Inst != chart.Bass {
		t.Fatalf("seats %d", len(p.seats))
	}
	rng := rand.New(rand.NewSource(2))
	h.until("band results", 200, sceneIs[*BandResults](h), mashOn(h, rng, 0, 1))
	r := a.scene.(*BandResults)
	if len(r.res) != 2 {
		t.Fatalf("%d results", len(r.res))
	}
	for _, sr := range r.res {
		if sr.rec.Profile != sr.m.prof.ID || sr.rec.Mode != modeBand || sr.rec.Players != 2 {
			t.Fatalf("recording not tagged: %+v", sr.rec.Profile)
		}
		if sr.path == "" {
			t.Fatal("run not saved")
		}
		if b := a.ScoresFor(sr.m.prof).Best(r.song.Dir, sr.rec.Instrument, sr.rec.Difficulty); b == nil {
			t.Fatalf("no best score for %s", sr.m.prof.Name)
		}
	}
	if r.res[0].path == r.res[1].path {
		t.Fatal("both runs saved to the same file")
	}
	// scores for player 2 rebuild from the runs folder by profile
	sc := replay.LoadScores(t.TempDir()+"/s.json", config.RunsDir(), m2.prof.Owns)
	if sc.Count() != 1 {
		t.Fatalf("player 2 has %d parts in a rebuilt index, want 1", sc.Count())
	}
	h.tap(bitB) // song list
	h.until("song list", 2, sceneIs[*SongSelect](h), nil)
	h.tap(bitB) // lobby, band kept
	h.until("lobby", 2, sceneIs[*Lobby](h), nil)
	if len(a.Band.Members) != 2 {
		t.Fatal("the band should still be together in the lobby")
	}
}

func TestBandOutAndFail(t *testing.T) {
	h := newHarness(t)
	withDemoLibrary(h)
	a := h.a
	a.Settings.Engine.NoFail = false
	b := newBand(modeBand)
	for i := 0; i < 2; i++ {
		b.join(a, device{src: input.SrcXInput, index: i})
	}
	a.Band = b
	a.scene = NewBandLoading(a, b, a.library[0])
	p := h.waitBandPlay()
	// player 1 is the bot, player 2 does nothing and runs out of rock
	p.seats[0].bot = botEvents(p.seats[0].t, false)
	h.until("player 2 out", 120, func() bool { return p.seats[1].out }, nil)
	if p.failed {
		t.Fatal("the song shouldn't fail while a player is still in")
	}
	// a save: player 1's star power brings player 2 back
	s2 := p.seats[1]
	s2.revive(p.songNow(h.now) - engineLag)
	if s2.out || s2.eng.Stats().Failed || s2.eng.Stats().Rock != 0.5 {
		t.Fatal("revive should bring the player back at half rock")
	}
	// with the bot gone too, everyone runs out and the song fails
	p.seats[0].bot = nil
	h.until("band results", 120, sceneIs[*BandResults](h), nil)
	r := a.scene.(*BandResults)
	if r.completed {
		t.Fatal("the song should have failed")
	}
	var rec *replay.Recording
	for _, sr := range r.res {
		if sr.m == b.Members[1] {
			rec = sr.rec
		}
	}
	if rec == nil || len(rec.Revives) != 1 {
		t.Fatal("player 2's recording should list the save")
	}
	// the recording replays to the same score with the save in it
	tr := replay.TrackFor(rec, p.c.Track(chart.Guitar, chart.Expert))
	e := replay.Resimulate(p.c, tr, rec.Settings, rec.Events(), rec.EndTime, rec.Revives...)
	if int64(e.Stats().Score+0.5) != rec.Result.Score || e.Stats().NotesMissed != rec.Result.NotesMissed {
		t.Fatalf("resimulated %v/%d, recorded %d/%d", e.Stats().Score, e.Stats().NotesMissed, rec.Result.Score, rec.Result.NotesMissed)
	}
}

func TestPartyDropInAndOut(t *testing.T) {
	h := newHarness(t)
	withDemoLibrary(h)
	a := h.a
	b := newBand(modeParty)
	b.join(a, device{src: input.SrcXInput, index: 0})
	a.Band = b
	a.scene = NewPartyNext(a, b)
	p := h.waitBandPlay()
	if !p.party {
		t.Fatal("not a party")
	}
	h.until("15 s in", 30, func() bool { return p.songNow(h.now) > 15 }, nil)
	h.tapOn(1, bitA) // someone picks up the second guitar
	if len(p.seats) != 2 || p.seats[1].joining == nil {
		t.Fatalf("a join card should appear (seats %d)", len(p.seats))
	}
	h.tapOn(1, bitA) // "Play!" is selected
	s := p.seats[1]
	if !s.playing() || s.first == 0 || s.joinedT < 15 {
		t.Fatalf("joined seat: playing %v first %d joined %.1f", s.playing(), s.first, s.joinedT)
	}
	if s.t.Notes[0].Time < s.joinedT+partyJoinLead-0.01 {
		t.Fatal("the first note should leave time to get ready")
	}
	h.idle(3)
	// player 2 opens their menu and switches to... leaving
	h.tapOn(1, bitStart)
	if s.menu == nil || p.paused {
		t.Fatal("Start opens the player's own menu without pausing")
	}
	for i := 0; i < 3; i++ {
		h.tapOn(1, bitDPadDown)
	}
	if s.menu == nil || s.menu.Sel != 3 {
		t.Fatalf("menu %v sel %d", s.menu != nil, func() int {
			if s.menu == nil {
				return -1
			}
			return s.menu.Sel
		}())
	}
	h.tapOn(1, bitA)
	if len(p.seats) != 1 || len(b.Members) != 1 {
		t.Fatalf("player 2 should be gone (seats %d)", len(p.seats))
	}
	// the song ends, the scores show, the next song starts by itself
	h.until("between songs", 120, func() bool { return p.endAt > 0 }, nil)
	h.until("next song", 30, func() bool {
		q, ok := a.scene.(*BandPlay)
		return ok && q != p
	}, nil)
}

// TestMultiplayerScreens draws the multiplayer screens for a look
// (JAM_SHOTS=<dir>; skipped otherwise).
func TestMultiplayerScreens(t *testing.T) {
	if testing.Short() || os.Getenv("JAM_SHOTS") == "" {
		t.Skip("set JAM_SHOTS to draw the multiplayer screens")
	}
	h := newHarness(t)
	withDemoLibrary(h)
	a := h.a
	a.NewProfile("Sam")
	a.scene = NewLobby(a)
	h.idle(0.1)
	h.tapOn(0, bitA)
	h.tapOn(1, bitA)
	h.frame(keyDown(keyEnter)) // the keyboard joins too
	h.frame(input.RawEvent{Src: input.SrcKeyboard, Kind: input.KindKey, Code: int(keyEnter)})
	l := a.scene.(*Lobby)
	h.tapOn(1, bitDPadDown)
	h.tapOn(1, bitLB) // bass
	h.tapOn(1, bitDPadDown)
	h.tapOn(1, bitDPadDown)
	h.tapOn(1, bitA) // ready
	h.idle(0.2)
	h.shot("mp-lobby")
	if len(l.band.Members) != 3 {
		t.Fatalf("%d joined", len(l.band.Members))
	}
	// the song list's band picker
	a.Band = l.band
	a.scene = NewSongSelect(a)
	h.idle(0.3)
	h.tap(bitA)
	h.idle(0.2)
	h.shot("mp-picker")

	// party: a join card and a player's own menu
	b := newBand(modeParty)
	b.join(a, device{src: input.SrcXInput, index: 0})
	b.join(a, device{src: input.SrcXInput, index: 2})
	a.Band = b
	a.scene = NewPartyNext(a, b)
	p := h.waitBandPlay()
	p.seats[0].bot = botEvents(p.seats[0].t, false)
	p.seats[1].bot = botEvents(p.seats[1].t, false)
	h.until("6 s in", 30, func() bool { return p.songNow(h.now) > 6 }, nil)
	h.tapOn(1, bitA) // a third player joins
	h.idle(0.3)
	h.shot("mp-party-join")
	h.tapOn(1, bitA) // Play!
	h.idle(1)
	h.shot("mp-party-joined")
	h.tapOn(2, bitStart)
	h.idle(0.3)
	h.shot("mp-party-menu")
	h.tapOn(2, bitA)
	p.finish(a, true)
	h.idle(0.5)
	h.shot("mp-party-between")
}

// TestMultiplayerScreens2 draws head to head, band results and profiles
// (JAM_SHOTS=<dir>; skipped otherwise).
func TestMultiplayerScreens2(t *testing.T) {
	if testing.Short() || os.Getenv("JAM_SHOTS") == "" {
		t.Skip("set JAM_SHOTS to draw the multiplayer screens")
	}
	h := newHarness(t)
	withDemoLibrary(h)
	a := h.a
	a.NewProfile("Sam")
	b2 := newBand(modeH2H)
	for i := 0; i < 3; i++ {
		b2.join(a, device{src: input.SrcXInput, index: i})
	}
	a.Band = b2
	a.scene = NewBandLoading(a, b2, a.library[0])
	q := h.waitBandPlay()
	for _, s := range q.seats {
		s.bot = botEvents(s.t, false)
	}
	q.seats[2].bot = nil // one player plays nothing
	h.until("6 s in", 30, func() bool { return q.songNow(h.now) > 6 }, nil)
	h.shot("mp-h2h")
	q.finish(a, true)
	h.idle(0.3)
	h.shot("mp-results")

	a.scene = NewProfileScene(a)
	h.idle(0.2)
	h.tap(bitDPadDown)
	h.tap(bitA)
	h.idle(0.2)
	h.shot("mp-profiles")
}
