# /// script
# requires-python = ">=3.11,<3.12"
# dependencies = ["chatterbox-tts", "soundfile", "setuptools<81"]
# ///
"""Speaks phrases with Chatterbox (Resemble AI, MIT licence), for voiceover.py --engine=chatterbox.

  uv run film/speak_chatterbox.py jobs.json

jobs.json is a list of {"text", "out", "seed"}; each phrase is written to its "out" WAV. The
model loads once for all of them. Chatterbox marks its audio with Resemble's inaudible Perth
watermark, which stays on (it needs setuptools, hence the pin above).
"""
import json, sys
import soundfile as sf
import torch
from chatterbox.tts import ChatterboxTTS

# Calmer than the defaults (0.5, 0.5): less dramatic, and a little slower.
EXAGGERATION, CFG_WEIGHT = 0.4, 0.3

jobs = json.load(open(sys.argv[1]))
device = "mps" if torch.backends.mps.is_available() else "cpu"
model = ChatterboxTTS.from_pretrained(device=device)
for i, job in enumerate(jobs):
    torch.manual_seed(job["seed"])
    wav = model.generate(job["text"], exaggeration=EXAGGERATION, cfg_weight=CFG_WEIGHT)
    sf.write(job["out"], wav.squeeze(0).cpu().numpy(), model.sr)
    print("[%d/%d] %s" % (i + 1, len(jobs), job["text"][:70]), flush=True)
