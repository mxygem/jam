# jam

A rhythm game in Go for guitar, bass and drum controllers, in the spirit of Guitar Hero and Rock Band. It plays Clone Hero / YARG song folders (`notes.chart` or `notes.mid`, `song.ini`, audio stems) and records every input of every run so you can see exactly what happened afterwards.

Built on [Ebitengine](https://ebitengine.org). Windows first; Linux and macOS build from the same code.

## Status

Working now:

- Guitar, bass, rhythm, co-op and 5-lane keys: strums, HOPOs (natural and forced), taps, open notes, chords with anchoring, sustains (extended and disjoint), star power phrases with whammy charge and tilt/button activation, solos.
- Drums: 4-lane, pro (cymbals vs toms) and 5-lane, Expert+ double kick, fills as star power activation, accents and ghosts shown.
- Charts: `.chart` and `notes.mid` (including Phase Shift SysEx taps/opens, `[ENHANCED_OPENS]`, tom markers, chart dynamics), `song.ini` overrides (`delay`, `hopo_frequency`, `eighthnote_hopo`, `sustain_cutoff_threshold`, `multiplier_note`, `pro_drums`, `five_lane_drums`).
- Audio: every stem plays in sync; your instrument's stem drops out when you miss. Overstrums make a noise by default; Settings > Audio picks which mistakes do (none, overstrums, extra hits, every miss) and how loud.
- Song previews: the song list plays the selected song from `song.ini`'s `preview_start_time` (to `preview_end_time` if set, otherwise 30 s), from a `preview` audio file if the folder has one, or else from the first chorus or a spot partway in. Settings > Audio sets their volume or turns them off.
- Audio formats: `.ogg`, `.mp3`, `.wav` play natively; `.opus`, `.flac`, `.m4a`, `.aiff` go through ffmpeg (`winget install ffmpeg`).
- Input: XInput guitars are polled on their own ~1 kHz thread with high resolution timestamps (CRKD guitars present as Guitar Hero style Xbox 360 guitars and work with the defaults). MIDI drum kits are the default drum input, with General MIDI note defaults. Keyboard and other gamepads work too.
- Setup wizard per instrument: detects the device, then each fret, strum, Star Power, tilt, whammy, Start and Select (or each pad, collecting every note a pad sends, like head and rim). Live test screen with a raw event log, then save.
- Latency calibration: audio (tap to a click track) and video (tap to a visual metronome).
- Recording and analysis: every run is saved with all inputs and judgments. The results screen shows a timing histogram, early/late bias, accuracy per section, accuracy per note type, every miss with a reason ("wrong frets: held --Y--, needed -R---", "120 ms too late", "hit the tom, chart has a cymbal"), sustains held, streak breaks, and tips. Runs re-simulate exactly from their inputs.
- Appearance: Settings > Appearance picks a theme (Jam, Neon, Arcade, Chrome, Pastel, Midnight, Gold, Glass, Color-blind friendly), a note material (classic, glossy, matte, metal, glass, neon, flat), rim, note size and highway style, and edits every color one by one (hue, saturation, brightness, quick picks or a hex code) while a song plays in a live preview. Changes make a custom look, which can be saved as a theme file to keep or share.
- Clock: the local time of day sits in the bottom right corner of every screen (of the window, on wider screens), in the current theme's colors.
- Lyrics: karaoke-style lines from `.chart` lyrics, `PART VOCALS` and `HARM1`-`HARM3`, or any number of vocal parts from a `chart.jam.json` project file next to the chart. Backing parts show below or above the lead, each in its own color. Charts can animate lyrics (see Lyric effects below); Settings > Highway and visuals > Lyric effects picks Full, Calm or Off.
- Local multiplayer: up to 8 players, each on their own highway, in one row or two rows of four (see Multiplayer below). Band, head to head and party modes.
- Profiles: every player has a name and color, their own gameplay settings, highway, calibration, controller setup, best scores and run history. Main menu > Profiles switches who's playing, makes, renames, recolors and deletes them.
- Clone Hero song folders are imported automatically from Clone Hero's `settings.ini` on first run. A built-in demo song is always available.

Planned: online play, custom art (highway and gem images), singing with a microphone, `.sng` packages, auto-charting from audio.

## Build

Requires Go 1.24+.

```sh
# Windows exe (no cgo needed), from Windows, WSL or Linux:
GOOS=windows GOARCH=amd64 go build -ldflags "-s -w" -o jam.exe ./cmd/jam

# Linux needs the usual Ebitengine packages:
#   libgl1-mesa-dev libxcursor-dev libxi-dev libxinerama-dev libxrandr-dev libxxf86vm-dev libasound2-dev
go build ./cmd/jam

go test ./...
```

## Play

```
jam                                   start the game
jam --song <folder> [--instrument drums] [--difficulty hard] [--autoplay]
jam --song <folder> --players 4 [--mode band|h2h|party] [--layout auto|row|rows] [--autoplay]
```

After a song, green goes back to the song list and yellow (or Tab) plays it again. Menus work from the keyboard (arrows, Enter, Esc), a guitar (strum to move, green to select, red to go back) or a drum kit (yellow up, blue down, green select, red back). F11 toggles fullscreen. Start or Esc pauses a song.

Default guitar keys if you play on a keyboard: A S D F G for frets, Up/Down arrows to strum, Space for Star Power.

## Multiplayer

Main menu > Multiplayer opens the lobby. Anyone joins by pressing Start or green on a guitar, green on a drum kit, or Enter on the keyboard; that controller is theirs until they leave. Each player sets up their own card with their own controller: profile (or a new one), instrument and difficulty, then Ready. Red leaves (or un-readies). The first player also picks the mode and layout.

- **Band**: one band score. A player whose rock meter runs out (with No fail off in their profile) is out until a bandmate activates star power, which brings them back at half rock. The song fails only if everyone is out at once.
- **Head to head**: everyone for themselves, ranked live on each highway and in the results. A player who fails stops; the song ends when everyone has.
- **Party**: songs play back to back with no failing. Songs come from the playlist being built (in order), then at random from the library, preferring songs with everyone's parts. Anyone can join mid-song by pressing Start on a free controller: they set up on a card in a new highway while the music keeps going, and their notes start 2.5 s later. Start on your own controller opens your menu (switch instrument or difficulty, vote to skip the song, or leave the party) without pausing anyone else. A skip vote shows on everyone's highway; Start votes yes, and the song skips once more than half the players agree (15 s to get there, then 10 s before another vote). Scores show for a few seconds between songs while the next one loads. Esc pauses (skip a song, or back to the lobby).

Layouts: Auto (up to 4 side by side, 5 to 8 in two rows of four), One row, or Two rows. Players pick from the parts their controller plays: a guitar any 5-fret part (guitar, bass, rhythm, keys, co-op), a drum kit drums, the keyboard anything. In band and head to head, the song list's picker shows each player's part for that song (left/right changes the instrument, green the difficulty); a player whose instrument the song doesn't have sits out. Any player's Start (or Esc) pauses. Results show every player's card; green opens one player's full analysis.

Controller mappings belong to profiles. Settings > Controls asks whose controls you're setting up, starting on whoever owns the controller that pressed green (a controller belongs to the profile that last joined with it or set it up; the keyboard belongs to no one). A player's lobby card has Controls to map their own controller straight away, and Profiles has guitar and drum setup for each profile. Joining on a controller brings back the profile it belongs to.

Every player's run is recorded under their profile. Band saves are part of the recording, so runs still re-simulate exactly. `jam --song <folder> --players 4 --autoplay [--mode h2h] [--layout rows]` plays a song with bots on every part (for trying layouts).

## Command line tools

```
jam analyze [run file] [--window 90]  print a run's analysis; --window re-simulates with a different hit window
jam probe [seconds]                   print raw XInput and MIDI events with timestamps (no window)
jam check [folder...]                 parse every chart in your library and run each track through the engine
jam demo [folder]                     write the demo song
```

## Lyric effects

Charts made for jam can animate their lyrics. Effects go inline in any lyric, and a tag can open in one syllable and close in a later one:

```
lyric <fx=shake:2>BURN      lyric IT      lyric DOWN</fx>
lyric <fx=color:#ff4d6d,glow>red</fx>
```

In `chart.jam.json` they also go in `fx` lists on a syllable, a phrase or a whole vocal part (`"fx": ["wave", "zoom:2"]`); a syllable's own list wins over its phrase's, which wins over its part's. Other games don't know these tags, so keep them to charts meant for jam.

| Effect | What it does |
|---|---|
| `color:#hex` | sung color for the syllable |
| `rainbow` | hue drifts across the sung letters |
| `glow[:size]` | halo around sung letters |
| `whisper` | smaller, italic, faded |
| `pulse` | the line breathes on the beat |
| `bounce` | each syllable hops as it's sung |
| `wave[:height]` | letters ride a slow wave |
| `shake[:n]` | letters jitter while sung |
| `zoom[:scale]` | the syllable pops big on its hit |
| `hold` | a held syllable's letters spread over the note |
| `glitch` | color channels split while sung |
| `type` | letters appear only as they're sung |
| `flyin` | letters drop into place before the phrase |
| `explode` | the syllable bursts into sparks after it's sung |
| `takeover` | the phrase fills the middle of the screen |

Calm leaves out shake, glitch, explode, wave, fly in and takeover.

## Where things live

Under `%AppData%\jam` on Windows (`~/.config/jam` on Linux; override with `JAM_HOME`):

- `settings.json`: library folders, multiplayer mode and layout, and the active profile's calibration, hit windows and highway shape
- `bindings.json`: the first profile's controller mappings per instrument family (guitar, drums)
- `scores.json`: the first profile's best scores
- `profiles/`: one `<id>.json` per profile (name, color, gameplay settings, highway, calibration), plus `<id>.bindings.json` and `<id>.scores.json` for every profile after the first
- `runs/`: one gzipped JSON file per playthrough with every input and judgment
- `songs/`: the demo song (and a good place for your own)
- `themes/`: theme files, one JSON file per theme (`name`, `colors` by slot as `#RRGGBB`, `material`, `rim`, `highway`, `highway_opacity`, `beat_lines`, `lane_lines`; anything left out comes from the Jam theme)

## Layout

```
cmd/jam            entry point and CLI tools
internal/chart     .chart / .mid / song.ini loaders, tempo map, song folders
internal/engine    deterministic gameplay rules (guitar, drums), scoring, star power
internal/input     XInput poller, MIDI (Windows, Linux), keyboard/gamepads, bindings, mapper
internal/playback  stem loading and sync, ffmpeg decoding, synthesized sound effects
internal/replay    run recording, re-simulation and analysis
internal/render    highway, gems, HUD, fonts
internal/profile   player profiles
internal/app       scenes: menus, song select, play, results, wizard, calibration, settings, lobby, multiplayer, profiles
internal/demo      built-in demo song
```

The engine is a discrete-event simulation: given a chart, settings and a list of timestamped inputs it always produces the same judgments, no matter how often it is advanced. The game keeps it about 40 ms behind real time so inputs that arrive a frame late are still processed in order.

## Credits

Fonts: Barlow and Barlow Condensed by The Barlow Project Authors, SIL Open Font License 1.1 (`internal/render/fonts/OFL.txt`). Chart format details follow the community documentation at [TheNathannator/GuitarGame_ChartFormats](https://github.com/TheNathannator/GuitarGame_ChartFormats).
