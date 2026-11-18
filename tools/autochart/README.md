# autochart

Audio file in, playable jam song folder out, with an Expert pro drums chart. Lyrics, bass and guitar
come back later.

```
python -m autochart.run "song.mp3" --artist "Artist" --title "Title"
python -m autochart.difficulties "<song folder>"   # optional: add Hard / Medium / Easy
```

autochart only makes Expert. Lower difficulties are a separate step, so they can be redone or made
another way without touching Expert. The song title gets " (autochart)" so it isn't mistaken for a
human chart of the same song.

Writes `<out-root>\<Artist> - <Title> (autochart)\` with `notes.chart`, `chart.jam.json`, `song.ini`,
`song.ogg` (the mix without drums) and `drums.ogg`. Default out-root on Windows is `E:\charts\autocharts`
(`--out-root` to change). Stems and caches go to `~/.cache/autochart/work/`.

## Pipeline

1. **Drum stem**: Demucs htdemucs, two stems (drums / everything else). `--drums STEM` skips it.
2. **Candidates** (`features.py`): very sensitive onset peaks in three band groups (catches every charted
   hit on the reference songs, about four candidates per real hit), then 160+ features per candidate:
   attack spectrum, level and decay over five spans up to 400 ms, band flux relative to the band's own
   recent hits, gaps to neighbours.
3. **Hit detectors** (`models.py`, `train.py`): gradient-boosted trees, one per group (kick, snare,
   cymbal, tom). Stage two adds the stage-one probabilities of neighbouring candidates (steady 8th hats,
   double-bass runs). Features are also standardized within the song, since mixes differ.
4. **Cymbal lane** (yellow hi-hat / blue ride / green crash): a separate classifier on detected cymbals,
   because lane choice is partly a charting decision, not just a sound.
5. **Beat grid** (`tempomap.py`): tempo per 12 s window from how well hits sit on an 8th grid, folded into
   90-180 BPM; dynamic-programming beat tracking with that local tempo; a half-beat phase check; beats
   nudged onto strong hits (follows live drift). Bars from where kicks, snares and crashes land, with a
   short pickup bar at the start when needed.
6. **Notes** (`chart.py`): snap to 16ths or triplets within 35 ms, else keep the real time.
6b. **Bars and time signatures** (`meter.py`): an 8th-note pulse over the beat grid; the song's main bar
   length from how its drum pattern repeats (7/4 for Cool 7, 5/4 for Days Into Nights); bars split by
   dynamic programming using a learned beat-1 classifier (`downbeat_rows.npz`, fitted at startup), pattern
   repetition, and bonuses for 4/4 and the main meter. Odd bars are written as x/8 (`TS 7 3`).
7. **Hard / Medium / Easy** (separate tool, `difficulties.py` + `reduce.py`): a classifier per difficulty
   predicts which Expert notes stay, learned from the official charts' own reductions.
8. **Output**: `.chart` (Clone Hero / YARG / jam) and `chart.jam.json` (`jam-project/1`, every note with
   `conf` and `src`).

## Setup

```
pip install -r requirements.txt
```

The trained model `autochart/model.pkl` is not in git (13 MB, rebuilt on every retrain, tied to the
scikit-learn version). Gem's copy is in `C:\Users\gemini\Games\jam\autochart\autochart\model.pkl`;
rebuild it with `tools/train_final.py`.

Paths: `AUTOCHART_REF` (reference song folders, default `C:\Users\gemini\Games\jam\autochart-reference`
on Windows), `AUTOCHART_DATA` (decoded audio and feature caches, default `~/.cache/autochart/refdata`).

## Training and testing

Reference songs live in `C:\Users\gemini\Games\jam\autochart-reference` (charts with separate drum stems).
`tools/prepref.py` decodes each into `drums_ref.wav` (official drum stems summed) and `mix.wav`;
Demucs on `mix.wav` gives `drums_sep.wav`, the realistic input.

- `tools/loso2.py --train sep+ref --test sep`: leave-one-song-out detector scores (each song tested by a
  model that never saw it).
- `tools/beateval.py`: beat grid vs the reference tempo map.
- `tools/reduceeval.py`: learned reductions vs the official Hard/Medium/Easy.
- `tools/train_final.py [--exclude SONG]`: train and save `autochart/model.pkl`.
- `tools/libprep.py training_candidates.csv`: turn library charts into training songs (Demucs on the GPU).
  Gem's recommended charters, in her order of preference: FigNeutered, Satan, tomato, Bloodline, Hoph2o,
  Fouriguy, deckra, AbstractOrigin, Boo, XEntombmentX, CyclopsDragon, Onyxite. Songs are weighted by that
  order and each charter's songs share at most 20 songs' worth of weight. Charts without pro-drum markings
  and duplicates of the reference songs are skipped.
- `tools/evallib.py`: train on library songs only, score the reference songs.
- `tools/scoretodo.py <reference root>`: autochart each reference from its own audio, score notes and bar lines.
- `tools/metereval2.py <dir>`: bar-line scores from chart files (leave-one-song-out downbeat model).
- `tools/report.py <folders>`: tempo range, time signatures and off-grid notes per autochart.
- `setup_wsl.sh`, `tools/pc_pipeline.sh`: GPU setup and the whole library run on Gem's PC (WSL).
- `tools/compare.py <notes.mid> <autochart folder>`: end-to-end score of a finished chart.

Set `OMP_NUM_THREADS=1` when something else is using the CPU (Demucs): OpenMP contention made training
25x slower.
