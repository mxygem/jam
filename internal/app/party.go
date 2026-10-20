package app

import (
	"fmt"
	"image/color"
	"log"
	"math"
	"math/rand"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/mxygem/jam/internal/action"
	"github.com/mxygem/jam/internal/chart"
	"github.com/mxygem/jam/internal/clock"
	"github.com/mxygem/jam/internal/input"
	"github.com/mxygem/jam/internal/profile"
	"github.com/mxygem/jam/internal/render"
)

// Party mode: songs play back to back with no failing, and anyone can
// join, switch parts or leave at any moment without stopping the music.
// Songs come from the playlist being built (in order) and then at random
// from the library, favoring songs that have everyone's parts.

// partyJoinLead is how long after joining a player's first note comes.
const partyJoinLead = 2.5

// betweenSongs is how long the scores show between party songs.
const betweenSongs = 6.0

// updateJoins lets players join mid-song by pressing confirm on a
// controller no one has.
func (p *BandPlay) updateJoins(a *App, byDev map[device][]input.RawEvent) {
	join, _ := p.joins.pressed(a, p.band, byDev)
	for _, d := range join {
		if len(p.band.Members) >= MaxPlayers {
			a.Toast(fmt.Sprintf("The party is full (%d players)", MaxPlayers))
			return
		}
		m := p.band.join(a, d)
		s := &seat{m: m, set: a.SettingsFor(m.prof)}
		s.joining = p.joinCard(a, s)
		p.seats = append(p.seats, s)
	}
}

// joinCard is the setup a new player goes through on their own highway.
func (p *BandPlay) joinCard(a *App, s *seat) *List {
	m := s.m
	return &List{Items: []Item{
		{Label: "Profile",
			Value: func() string {
				if s.newProf {
					return "+ New profile"
				}
				return m.prof.Name
			},
			Swatch: func() color.RGBA {
				if s.newProf {
					return th.TextDim
				}
				return profileColor(m.prof)
			},
			Adjust: func(d int) { s.newProf = cycleBandProfile(a, p.band, m, s.newProf, d) }},
		{Label: "Instrument", Value: func() string { return m.inst.Title() }, Adjust: func(d int) { m.inst = cyclePart(m.inst, d, m.parts(p.songParts())) }},
		{Label: "Difficulty", Value: func() string { return m.diff.Title() }, Adjust: func(d int) { m.diff = cycleDiff(m.diff, d) }},
		{Label: "Play!", Activate: func() {
			if s.newProf {
				m.setProfile(a, a.NewProfile(""))
				s.newProf = false
			}
			m.remember(a)
			p.reseat(a, s, false)
		}},
		{Label: "Cancel", Activate: func() { s.left = true }},
	}, Visible: 5, Sel: 3}
}

// cycleBandProfile steps a member through profiles no one else in the band
// has, then "New profile". It returns whether "New profile" is chosen.
func cycleBandProfile(a *App, b *Band, m *member, onNew bool, d int) bool {
	var opts []*profile.Profile
	for _, p := range a.Profiles.All() {
		if p == m.prof || !b.has(p) {
			opts = append(opts, p)
		}
	}
	opts = append(opts, nil)
	i := 0
	for j, p := range opts {
		if (onNew && p == nil) || (!onNew && p == m.prof) {
			i = j
		}
	}
	n := len(opts)
	next := opts[((i+sign(d))%n+n)%n]
	if next == nil {
		return true
	}
	m.setProfile(a, next)
	return false
}

// songParts lists the instruments the current song has.
func (p *BandPlay) songParts() []chart.Instrument {
	var out []chart.Instrument
	for _, inst := range partChoices {
		if len(p.c.Available(inst)) > 0 {
			out = append(out, inst)
		}
	}
	return out
}

// reseat (re)starts a member's seat partway through the song, with their
// current part. The old seat's run is kept when it played anything.
func (p *BandPlay) reseat(a *App, old *seat, keepOld bool) {
	now := p.songNow(clock.Now())
	if keepOld && old.eng != nil {
		p.keep(a, old, now, false, true)
	}
	from := math.Max(0, now+partyJoinLead)
	if !p.started {
		from = 0
	}
	s := newSeat(a, p, old.m, from)
	if from > 0 {
		s.joinedT = now
	}
	s.view = old.view
	for i, x := range p.seats {
		if x == old {
			p.seats[i] = s
		}
	}
	log.Printf("party: %s plays %s from %.1fs", s.m.prof.Name, s.m.partLabel(), from)
}

// openSeatMenu opens a player's own menu (party mode, Start on their
// controller): switch part or difficulty, or leave.
func (p *BandPlay) openSeatMenu(a *App, s *seat) {
	inst, diff := s.m.inst, s.m.diff
	apply := func() {
		s.menu = nil
		if s.m.inst != inst || s.m.diff != diff {
			s.m.remember(a)
			p.reseat(a, s, true)
		}
	}
	s.menu = &List{Items: []Item{
		{Label: "Back to playing", Activate: apply},
		{Label: "Instrument", Value: func() string { return s.m.inst.Title() }, Adjust: func(d int) { s.m.inst = cyclePart(s.m.inst, d, s.m.parts(p.songParts())) }},
		{Label: "Difficulty", Value: func() string { return s.m.diff.Title() }, Adjust: func(d int) { s.m.diff = cycleDiff(s.m.diff, d) }},
		{Label: "Vote to skip this song",
			Hint: "Skips when more than half the players vote yes",
			Value: func() string {
				switch {
				case p.vote != nil && p.vote.yes[s.m]:
					return "You voted"
				case p.vote != nil:
					return fmt.Sprintf("%d of %d", p.yesVotes(), votesNeeded(len(p.voters())))
				case !p.canStartVote(clock.Now()):
					return "Wait a moment"
				}
				return ""
			},
			Activate: func() { apply(); p.voteSkip(a, s.m) }},
		{Label: "Leave the party", Activate: func() { s.left = true; s.menu = nil }},
	}, Visible: 5}
}

// updateSeatMenu runs a seat's own menu (or join card) from its controller.
func (p *BandPlay) updateSeatMenu(a *App, s *seat, acts []action.Action) {
	if len(acts) == 0 {
		return
	}
	l := s.menu
	if s.joining != nil {
		l = s.joining
	}
	used := false
	asMember(a, acts, func() { used = l.Update(a) })
	if used {
		return
	}
	for _, act := range acts {
		if act != action.MenuBack {
			continue
		}
		if s.joining != nil {
			s.left = true
		} else if s.menu != nil {
			s.menu.Items[0].Activate() // back to playing
		}
		return
	}
}

// drop removes players who left, keeping what they played.
func (p *BandPlay) drop(a *App) {
	keep := p.seats[:0]
	for _, s := range p.seats {
		if !s.left {
			keep = append(keep, s)
			continue
		}
		if s.eng != nil {
			p.keep(a, s, p.songNow(clock.Now()), false, true)
		}
		p.band.remove(s.m)
		if s.view != nil {
			s.view.Deallocate()
		}
		log.Printf("party: %s left", s.m.prof.Name)
		a.Toast(s.m.prof.Name + " left the party")
	}
	p.seats = keep
}

// --- between songs ---

// partyLoad is the next party song loading in the background.
type partyLoad struct {
	song  *chart.Song
	done  chan loadResult
	res   *loadResult
	tries int
}

func (pl *partyLoad) ready() bool {
	if pl == nil || pl.song == nil {
		return false
	}
	if pl.res == nil {
		select {
		case r := <-pl.done:
			pl.res = &r
		default:
		}
	}
	return pl.res != nil
}

// startPartyLoad picks the next song and starts loading it.
func startPartyLoad(a *App, b *Band) *partyLoad {
	s := a.partyPick(b)
	if s == nil {
		return &partyLoad{}
	}
	b.played = append(b.played, dirKey(s.Dir))
	if len(b.played) > 50 {
		b.played = b.played[len(b.played)-50:]
	}
	log.Printf("party: next song %s", s.Title())
	return &partyLoad{song: s, done: loadSong(a, s)}
}

// partyPick chooses the next party song: the playlist's next entry, or a
// random library song that has as many of the players' parts as possible.
func (a *App) partyPick(b *Band) *chart.Song {
	if a.Queue != nil {
		for b.queueIdx < len(a.Queue.Entries) {
			e := a.Queue.Entries[b.queueIdx]
			b.queueIdx++
			if s := a.SongByDir(e.Dir); s != nil {
				return s
			}
		}
	}
	songs, _ := a.Library()
	if len(songs) == 0 {
		return nil
	}
	recent := map[string]bool{}
	for _, d := range b.played {
		recent[d] = true
	}
	var best *chart.Song
	bestScore := -1
	for try := 0; try < 400 && bestScore < len(b.Members)+1; try++ {
		s := songs[rand.Intn(len(songs))]
		if recent[dirKey(s.Dir)] && try < 300 {
			continue
		}
		in := a.Index.Get(s)
		score := 0
		if in != nil {
			if in.Err != "" || len(in.Parts) == 0 {
				continue
			}
			score = 1 // known to be playable
			for _, m := range b.Members {
				if in.Part(m.inst) != nil {
					score++
				}
			}
		}
		if score > bestScore {
			best, bestScore = s, score
		}
	}
	return best
}

// betweenSongs shows this song's scores while the next one loads.
func (p *BandPlay) betweenSongs(a *App) {
	p.endAt = clock.Now()
	p.au.Pause()
	p.next = startPartyLoad(a, p.band)
}

func (p *BandPlay) updateBetween(a *App, now float64) error {
	byDev := splitInput(a.Raw)
	join, _ := p.joins.pressed(a, p.band, byDev)
	for _, d := range join {
		if len(p.band.Members) < MaxPlayers {
			m := p.band.join(a, d)
			a.Toast(m.prof.Name + " joins on the next song (" + m.partLabel() + "). START to change it")
		}
	}
	for _, ev := range a.Raw {
		if ev.Src == input.SrcKeyboard && ebiten.Key(ev.Code) == ebiten.KeyEscape && ev.Down {
			p.au.Close()
			a.SetScene(NewLobby(a))
			return nil
		}
	}
	nl := p.next
	if nl.song == nil {
		if now-p.endAt > 2 {
			p.au.Close()
			a.Toast("No songs to play: add song folders in Settings")
			a.SetScene(NewLobby(a))
		}
		return nil
	}
	if !nl.ready() {
		return nil
	}
	if nl.res.err != nil {
		log.Printf("party: %s won't load: %v", nl.song.Title(), nl.res.err)
		if nl.tries >= 5 {
			p.au.Close()
			a.Toast("Couldn't load a song: " + nl.res.err.Error())
			a.SetScene(NewLobby(a))
			return nil
		}
		tries := nl.tries + 1
		p.next = startPartyLoad(a, p.band)
		p.next.tries = tries
		return nil
	}
	if now-p.endAt >= betweenSongs || len(p.results) == 0 {
		p.au.Close()
		a.SetScene(NewBandPlay(a, p.band, nl.song, nl.res.c, nl.res.audio))
	}
	return nil
}

func (p *BandPlay) drawBetween(dst *ebiten.Image, a *App) {
	render.Dim(dst, render.RGBA(0, 0, 0, 0xB0))
	render.TextShadow(dst, render.PlainText(p.song.Name), render.UIBold, 56, render.ScreenW/2, 150, th.Text, render.Center)
	render.Text(dst, render.PlainText(p.song.Artist), render.UI, 32, render.ScreenW/2, 224, th.TextDim, render.Center)
	n := len(p.results)
	cw := math.Min(420, (render.ScreenW-120)/float64(max(1, min(n, 4))))
	for i, r := range p.results {
		row, col := i/4, i%4
		inRow := min(4, n-row*4)
		x := (render.ScreenW-cw*float64(inRow))/2 + float64(col)*cw
		y := 320 + float64(row)*270
		Panel(dst, x+10, y, cw-20, 240)
		render.FillRect(dst, x+10, y, cw-20, 8, profileColor(r.m.prof))
		render.Text(dst, truncateW(r.m.prof.Name, render.UIBold, 34, cw-60), render.UIBold, 34, x+30, y+24, th.Text, render.Left)
		part := chartTitle(r.rec.Difficulty) + " " + chartTitle(r.rec.Instrument)
		if r.left {
			part += " (left)"
		}
		render.Text(dst, part, render.Cond, 24, x+30, y+70, th.TextDim, render.Left)
		render.Text(dst, fmt.Sprintf("%d", r.rec.Result.Score), render.Display, 52, x+30, y+110, th.Text, render.Left)
		stats := fmt.Sprintf("%.1f%%  ·  %d streak", r.rec.Result.HitRate()*100, r.rec.Result.MaxStreak)
		if r.rec.Result.Reached() == 0 {
			stats = "Just joined"
		}
		render.Text(dst, stats, render.Cond, 26, x+30, y+180, th.TextDim, render.Left)
	}
	msg := "Next song coming up..."
	if p.next != nil && p.next.song != nil {
		msg = "Next: " + render.PlainText(p.next.song.Name) + "  ·  " + render.PlainText(p.next.song.Artist)
	}
	render.Text(dst, truncateW(msg, render.UI, 32, 1700), render.UI, 32, render.ScreenW/2, 900, th.Text, render.Center)
	Footer(dst, "Press START or green on any controller to join     Esc back to the lobby")
}

// --- the first party song ---

// PartyStart loads the first song of a party.
type PartyStart struct {
	band  *Band
	next  *partyLoad
	start float64
}

// NewPartyNext starts a party: the first song loads, then play begins.
func NewPartyNext(a *App, b *Band) *PartyStart {
	if b == nil {
		b = a.Band
	}
	b.Mode = modeParty
	return &PartyStart{band: b, start: clock.Now()}
}

func (ps *PartyStart) Update(a *App) error {
	if a.Pressed(action.MenuBack) {
		a.SetScene(NewLobby(a))
		return nil
	}
	if ps.next == nil || ps.next.song == nil {
		songs, scanning := a.Library()
		if len(songs) == 0 {
			if !scanning && clock.Now()-ps.start > 1 {
				a.Toast("No songs to play: add song folders in Settings")
				a.SetScene(NewLobby(a))
			}
			return nil
		}
		ps.next = startPartyLoad(a, ps.band)
		return nil
	}
	if !ps.next.ready() {
		return nil
	}
	if err := ps.next.res.err; err != nil {
		log.Printf("party: %s won't load: %v", ps.next.song.Title(), err)
		tries := ps.next.tries + 1
		if tries > 5 {
			a.Toast("Couldn't load a song: " + err.Error())
			a.SetScene(NewLobby(a))
			return nil
		}
		ps.next = startPartyLoad(a, ps.band)
		ps.next.tries = tries
		return nil
	}
	a.SetScene(NewBandPlay(a, ps.band, ps.next.song, ps.next.res.c, ps.next.res.audio))
	return nil
}

func (ps *PartyStart) Draw(dst *ebiten.Image, a *App) {
	MenuBackground(dst)
	render.TextShadow(dst, "Party", render.Display, 120, render.ScreenW/2, 300, th.Text, render.Center)
	msg := "Picking a song..."
	if ps.next != nil && ps.next.song != nil {
		msg = render.PlainText(ps.next.song.Name) + "  ·  " + render.PlainText(ps.next.song.Artist)
	}
	render.Text(dst, truncateW(msg, render.UI, 36, 1600), render.UI, 36, render.ScreenW/2, 470, th.TextDim, render.Center)
	names := make([]string, 0, len(ps.band.Members))
	for _, m := range ps.band.Members {
		names = append(names, m.prof.Name)
	}
	render.Text(dst, strings.Join(names, "  ·  "), render.Cond, 30, render.ScreenW/2, 560, th.Text, render.Center)
	Footer(dst, "Anyone can join during songs: press START or green on a controller     Esc back")
}

// --- skip votes ---

// A party player can start a vote to skip the song from their menu. While
// it's open, everyone else sees it on their highway and votes yes with
// Start. It passes with more than half of the players and closes after
// skipVoteTime seconds of song time otherwise.

const (
	skipVoteTime     = 15.0
	skipVoteCooldown = 10.0 // after a vote fails, before another can start
)

type skipVote struct {
	yes   map[*member]bool
	until float64 // song time it closes
}

// voters is how many players get a say: everyone with a highway, not those
// still setting up after joining.
func (p *BandPlay) voters() []*member {
	var out []*member
	for _, s := range p.seats {
		if s.joining == nil && !s.left {
			out = append(out, s.m)
		}
	}
	return out
}

// votesNeeded is a majority of the players.
func votesNeeded(players int) int { return players/2 + 1 }

func (p *BandPlay) yesVotes() int {
	n := 0
	for _, m := range p.voters() {
		if p.vote != nil && p.vote.yes[m] {
			n++
		}
	}
	return n
}

// canStartVote reports whether a new skip vote can start now.
func (p *BandPlay) canStartVote(now float64) bool {
	return p.vote == nil && (p.voteFailedAt == 0 || p.songNow(now)-p.voteFailedAt >= skipVoteCooldown)
}

// voteSkip starts a skip vote, or adds the player's yes to the open one.
func (p *BandPlay) voteSkip(a *App, m *member) {
	now := clock.Now()
	if p.vote == nil {
		if !p.canStartVote(now) {
			return
		}
		p.vote = &skipVote{yes: map[*member]bool{}, until: p.songNow(now) + skipVoteTime}
		log.Printf("party: %s started a vote to skip %s", m.prof.Name, p.song.Title())
	}
	p.vote.yes[m] = true
}

// startPressed is Start on a party player's controller: their yes while a
// skip vote waits on them, otherwise their menu.
func (p *BandPlay) startPressed(a *App, s *seat) {
	if p.vote != nil && !p.vote.yes[s.m] {
		p.voteSkip(a, s.m)
		return
	}
	p.openSeatMenu(a, s)
}

// updateVote passes or closes the open skip vote.
func (p *BandPlay) updateVote(a *App, now float64) {
	if p.vote == nil {
		return
	}
	players := len(p.voters())
	yes := p.yesVotes()
	switch {
	case players > 0 && yes >= votesNeeded(players):
		log.Printf("party: skip vote passed %d/%d", yes, players)
		p.vote = nil
		a.Toast("Skipped by vote")
		p.finish(a, false)
	case p.songNow(now) > p.vote.until || players == 0:
		log.Printf("party: skip vote failed %d/%d", yes, players)
		p.vote = nil
		p.voteFailedAt = p.songNow(now)
		a.Toast(fmt.Sprintf("Not enough votes to skip (%d of %d needed)", yes, votesNeeded(players)))
	}
}

// voteLabel is the top bar while a vote is open.
func (p *BandPlay) voteLabel(now float64) string {
	left := math.Max(0, p.vote.until-p.songNow(now))
	return fmt.Sprintf("SKIP THIS SONG?  %d of %d needed  ·  START to vote  ·  %.0f", p.yesVotes(), votesNeeded(len(p.voters())), math.Ceil(left))
}
