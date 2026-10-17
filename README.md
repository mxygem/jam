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
- Clone Hero song folders are imported automatically from Clone Hero's `settings.ini` on first run. A built-in demo song is always available.

Planned: custom art (highway and gem images), vocals, `.sng` packages, auto-charting from audio.

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
```

Menus work from the keyboard (arrows, Enter, Esc), a guitar (strum to move, green to select, red to go back) or a drum kit (yellow up, blue down, green select, red back). F11 toggles fullscreen. Start or Esc pauses a song.

Default guitar keys if you play on a keyboard: A S D F G for frets, Up/Down arrows to strum, Space for Star Power.

## Command line tools

```
jam analyze [run file] [--window 90]  print a run's analysis; --window re-simulates with a different hit window
jam probe [seconds]                   print raw XInput and MIDI events with timestamps (no window)
jam check [folder...]                 parse every chart in your library and run each track through the engine
jam demo [folder]                     write the demo song
```

## Where things live

Under `%AppData%\jam` on Windows (`~/.config/jam` on Linux; override with `JAM_HOME`):

- `settings.json`: library folders, calibration, hit windows, highway shape
- `bindings.json`: controller mappings per instrument family (guitar, drums)
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
internal/app       scenes: menus, song select, play, results, wizard, calibration, settings
internal/demo      built-in demo song
```

The engine is a discrete-event simulation: given a chart, settings and a list of timestamped inputs it always produces the same judgments, no matter how often it is advanced. The game keeps it about 40 ms behind real time so inputs that arrive a frame late are still processed in order.

## Credits

Fonts: Barlow and Barlow Condensed by The Barlow Project Authors, SIL Open Font License 1.1 (`internal/render/fonts/OFL.txt`). Chart format details follow the community documentation at [TheNathannator/GuitarGame_ChartFormats](https://github.com/TheNathannator/GuitarGame_ChartFormats).
