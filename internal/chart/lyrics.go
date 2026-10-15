package chart

import (
	"sort"
	"strings"

	"github.com/mxygem/jam/internal/rich"
)

// Syllable is one lyric event as it should be displayed.
type Syllable struct {
	Time float64
	Text string // display text (may contain rich text tags)
	Join bool   // no space before the next syllable
}

// LyricPhrase is one line of lyrics.
type LyricPhrase struct {
	Start, End float64
	Syllables  []Syllable
}

// Line returns the whole phrase as display text.
func (p *LyricPhrase) Line() string {
	var b strings.Builder
	for i, s := range p.Syllables {
		b.WriteString(s.Text)
		if !s.Join && i < len(p.Syllables)-1 {
			b.WriteByte(' ')
		}
	}
	return b.String()
}

// cleanSyllable applies the lyric symbols used by Rock Band, Clone Hero and
// YARG charts. It returns the display text, whether it joins the next
// syllable without a space, and false for syllables that aren't shown.
//
//	word-   joined to the next syllable          word=  shown with a hyphen, joined
//	+       pitch slide, not a new syllable      _      shown as a space
//	# ^ * % $ / §  pitch, talkie and harmony markers (removed)
func cleanSyllable(raw string) (string, bool, bool) {
	s := strings.TrimSpace(raw)
	if s == "" || s == "+" {
		return "", false, false
	}
	// Symbols apply to the text, never inside rich text tags.
	pieces, isTag := rich.Split(s)
	lastText := -1
	for i := range pieces {
		if !isTag[i] {
			lastText = i
		}
	}
	join := false
	if lastText >= 0 {
		t := strings.TrimRight(pieces[lastText], " ")
		switch {
		case strings.HasSuffix(t, "-"):
			join = true
			t = strings.TrimSuffix(t, "-")
		case strings.HasSuffix(t, "="):
			join = true
			t = strings.TrimSuffix(t, "=") + "\x00" // becomes a shown hyphen
		}
		pieces[lastText] = t
	}
	var b strings.Builder
	visible := false
	for i, p := range pieces {
		if isTag[i] {
			b.WriteString(p)
			continue
		}
		p = strings.Map(func(r rune) rune {
			switch r {
			case '#', '^', '*', '%', '$', '/', '+':
				return -1
			case '_', '§':
				return ' '
			case '=', 0:
				return '-'
			}
			return r
		}, p)
		if strings.TrimSpace(p) != "" {
			visible = true
		}
		b.WriteString(p)
	}
	if !visible {
		return "", join, false
	}
	return strings.TrimSpace(b.String()), join, true
}

type rawLyric struct {
	tick int64
	text string
}

// buildLyrics groups lyric events into phrases. spans are phrase ranges in
// ticks (from phrase markers); without them, lines are split on long gaps.
func buildLyrics(tm *TempoMap, lyrics []rawLyric, spans []span) []LyricPhrase {
	if len(lyrics) == 0 {
		return nil
	}
	sort.SliceStable(lyrics, func(i, j int) bool { return lyrics[i].tick < lyrics[j].tick })
	var phrases []LyricPhrase
	add := func(from, to int64, ls []rawLyric) {
		p := LyricPhrase{Start: tm.Seconds(from), End: tm.Seconds(to)}
		for _, l := range ls {
			text, join, ok := cleanSyllable(l.text)
			if !ok {
				// A slide continues the previous syllable; keep its join.
				continue
			}
			p.Syllables = append(p.Syllables, Syllable{Time: tm.Seconds(l.tick), Text: text, Join: join})
		}
		if len(p.Syllables) > 0 {
			phrases = append(phrases, p)
		}
	}
	if len(spans) > 0 {
		sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
		i := 0
		for _, sp := range spans {
			for i < len(lyrics) && lyrics[i].tick < sp.start {
				i++ // lyrics outside any phrase are dropped, as in-game
			}
			j := i
			for j < len(lyrics) && lyrics[j].tick <= sp.end {
				j++
			}
			add(sp.start, sp.end, lyrics[i:j])
			i = j
		}
		return phrases
	}
	// No phrase markers: break lines at gaps of over 1.2 s or every 8 words.
	start := 0
	words := 0
	for i := range lyrics {
		if i > start {
			gap := tm.Seconds(lyrics[i].tick) - tm.Seconds(lyrics[i-1].tick)
			if gap > 1.2 || words >= 8 {
				add(lyrics[start].tick, lyrics[i-1].tick, lyrics[start:i])
				start, words = i, 0
			}
		}
		if _, join, ok := cleanSyllable(lyrics[i].text); ok && !join {
			words++
		}
	}
	add(lyrics[start].tick, lyrics[len(lyrics)-1].tick, lyrics[start:])
	return phrases
}

// lyricsFromEvents reads .chart style global events: "phrase_start",
// "lyric text", "phrase_end".
func lyricsFromEvents(tm *TempoMap, events []TextEvent) []LyricPhrase {
	var lyrics []rawLyric
	var spans []span
	open := int64(-1)
	for _, e := range events {
		t := strings.TrimSpace(e.Text)
		switch {
		case strings.HasPrefix(t, "lyric "):
			lyrics = append(lyrics, rawLyric{e.Tick, t[len("lyric "):]})
		case t == "phrase_start":
			if open >= 0 {
				spans = append(spans, span{open, e.Tick})
			}
			open = e.Tick
		case t == "phrase_end":
			if open >= 0 {
				spans = append(spans, span{open, e.Tick})
				open = -1
			}
		}
	}
	if open >= 0 && len(lyrics) > 0 {
		spans = append(spans, span{open, lyrics[len(lyrics)-1].tick})
	}
	return buildLyrics(tm, lyrics, spans)
}

// lyricsFromVocals reads a PART VOCALS track: lyric/text meta events at the
// sung notes, phrases from notes 105/106.
func lyricsFromVocals(tm *TempoMap, t smfTrack) []LyricPhrase {
	var lyrics []rawLyric
	for _, e := range t.events {
		if !e.isText() || (e.meta != 0x05 && e.meta != 0x01) {
			continue
		}
		text := string(e.data)
		if strings.HasPrefix(strings.TrimSpace(text), "[") {
			continue // [play], [idle] and other animation events
		}
		lyrics = append(lyrics, rawLyric{e.tick, text})
	}
	// Some files carry every lyric twice (as lyric and text events).
	sort.SliceStable(lyrics, func(i, j int) bool { return lyrics[i].tick < lyrics[j].tick })
	dedup := lyrics[:0]
	for i, l := range lyrics {
		if i > 0 && l.tick == lyrics[i-1].tick && l.text == lyrics[i-1].text {
			continue
		}
		dedup = append(dedup, l)
	}
	notes := pairNotes(t.events)
	spans := append(notesToSpans(notes, 105), notesToSpans(notes, 106)...)
	return buildLyrics(tm, dedup, spans)
}
