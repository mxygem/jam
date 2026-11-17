#!/usr/bin/env bash
# One-time setup for autochart in WSL (Ubuntu-24.04): Python 3.12 venv with CUDA torch and Demucs.
#   wsl -d Ubuntu-24.04 -- bash /mnt/c/Users/gemini/Games/jam/autochart/setup_wsl.sh
set -euo pipefail
export PATH="$HOME/.local/bin:$PATH"
if ! command -v uv >/dev/null; then
  curl -LsSf https://astral.sh/uv/install.sh | sh
fi
VENV="$HOME/autochart-venv"
[ -d "$VENV" ] || uv venv -p 3.12 "$VENV"
. "$VENV/bin/activate"
uv pip install torch torchaudio --index-url https://download.pytorch.org/whl/cu124
uv pip install demucs librosa soundfile scikit-learn mido numpy scipy
# Demucs model: reuse the copy already downloaded on Windows
mkdir -p "$HOME/autochart-models"
cp -n /mnt/c/Users/gemini/Games/jam/autochart-models/955717e8-8726e21a.th "$HOME/autochart-models/"
echo "models: ['955717e8']" > "$HOME/autochart-models/htdemucs.yaml"
python -c "import torch, demucs, sklearn, librosa; print('torch', torch.__version__, 'cuda', torch.cuda.is_available(), torch.cuda.get_device_name(0) if torch.cuda.is_available() else '')"
echo SETUP_OK
