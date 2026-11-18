#!/usr/bin/env bash
# Runs in WSL: prepare reference songs + library songs (Demucs on the GPU), then train on the library
# and score the reference songs. Log: /mnt/c/Users/gemini/Games/jam/autochart/pipeline.log
set -uo pipefail
. "$HOME/autochart-venv/bin/activate"
cd /mnt/c/Users/gemini/Games/jam/autochart
export AUTOCHART_DATA="$HOME/autochart-data" AUTOCHART_REF=/mnt/c/Users/gemini/Games/jam/autochart-reference
mkdir -p "$AUTOCHART_DATA"
for d in "$AUTOCHART_REF"/*/; do
  s=$(basename "$d"); out="$AUTOCHART_DATA/$s"
  [ -f "$out/drums_sep.wav" ] && continue
  python tools/prepref.py "$d" "$out" && \
  python -m demucs --repo "$HOME/autochart-models" -n htdemucs --two-stems drums -o "$out/demucs" "$out/mix.wav" >/dev/null 2>&1 && \
  mv "$out/demucs/htdemucs/mix/drums.wav" "$out/drums_sep.wav" && rm -rf "$out/demucs" && echo "ref ok: $s"
done
python tools/libprep.py training_candidates.csv
OMP_NUM_THREADS=$(nproc) python tools/evallib.py
echo PIPELINE_DONE
