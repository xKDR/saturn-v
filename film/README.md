# The getting-started film

A narrated film of just under three minutes for people starting with saturn: getting access, connecting, querying the datasets, what a job is, why it needs `renv.lock` / `Project.toml`, and sending one. It looks like a screen recording, but every window is a hand-built replica that animates; the commands, their output, the query results, the job's log, its cost and the Zulip notice come from a real run (3 Nov 2026).

| File | What |
| --- | --- |
| `template.html` | The film: a 1600×900 stage where every frame is computed from the time alone (`render(t)`) |
| `voiceover.py` | The narration, and the timeline that follows from how long each line takes. Kokoro by default; Chatterbox with `--engine=chatterbox` |
| `speak_chatterbox.py` | Chatterbox itself, run by `voiceover.py` with its own dependencies |
| `build.mjs` | Puts the logo, the job's chart and the timeline into the page: `out/saturn-v.html` |
| `export.mjs` | Renders the page to MP4 through headless Chrome and ffmpeg |
| `job.json` | What saturn printed for the real job, and its Zulip notice |
| `xkdr-logo.svg` | The xKDR wordmark, as on xkdr.org |

## Making it

Needs Chrome, ffmpeg, Node and [uv](https://docs.astral.sh/uv/) (and [Rubber Band](https://breakfastquay.com/rubberband/) for Chatterbox). Kokoro's model files go in `out/models` once:

```sh
mkdir -p film/out/models && cd film/out/models
curl -fLO https://github.com/thewh1teagle/kokoro-onnx/releases/download/model-files-v1.0/kokoro-v1.0.onnx
curl -fLO https://github.com/thewh1teagle/kokoro-onnx/releases/download/model-files-v1.0/voices-v1.0.bin
```

Then, from the repository root:

```sh
uv run film/voiceover.py --check      # speak each line, write the timeline, Whisper reads them back
node film/build.mjs                   # out/saturn-v.html: open it to watch the picture
node film/export.mjs                  # out/saturn-v.mp4, silent, 1920 wide (about 5 minutes)
python3 <add-music.py> --audio=<track> --video=film/out/saturn-v.mp4 --out=film/out/saturn-v-scored.mp4 --dur=<DUR> --lufs=-16
uv run film/voiceover.py --mix        # out/saturn-v-narrated.mp4: narration, music at the open and close
```

`node film/export.mjs --stills=2,30,60` renders single frames to `out/stills/` for checking the picture before a full export. The music is the AccioPix track, fitted to the film by eventzoom's `scripts/ads/spot/add-music.py`; neither is in this repository.

## Directing the voice

The film uses Kokoro's `af_heart` voice. Chatterbox (`--engine=chatterbox`, Resemble AI, MIT licence) was tried and set aside: it talks at about 230 words a minute with no speed setting, and stretching it to a normal pace made it sound electronic and uneven. Copying the pace and voice of a real reference recording would avoid the stretching, if it's ever wanted. Its audio carries Resemble's inaudible Perth watermark; the first run downloads PyTorch and the model, about 2 GB.

Each line in `voiceover.py` has a pace (`quick`, `normal`, `land`), and inside a line `[slow]…[/slow]` and `[fast]…[/fast]` change the speed of a phrase and `[pause 0.4]` adds that much silence. Kokoro takes no spoken-style directions, so emphasis is a slower phrase with a short pause before it. `SPOKEN` respells words it would otherwise say wrong ("R-env", "Duck D B", "sequel"); `CB_SPOKEN` adds commas where Chatterbox runs words together, and `PHONEMES` softens Kokoro's stress on "job". Change a line and its beat re-times itself; never edit the timeline by hand.
