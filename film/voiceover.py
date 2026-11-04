# /// script
# requires-python = ">=3.11,<3.14"
# dependencies = ["kokoro-onnx>=0.4", "soundfile", "numpy", "faster-whisper"]
# ///
"""Narration for the getting-started film, spoken by Kokoro (a local model).

One clip per line, so every sentence lands on its beat. The film's timeline is then
derived from the clip lengths (each beat lasts at least as long as its line plus a
breath) and written to out/vo/timing.json, which build.mjs puts into the page:

  uv run film/voiceover.py                 # clips, timing.json and a report
  uv run film/voiceover.py --check         # Whisper reads each clip back
  node film/build.mjs && node film/export.mjs
  uv run film/voiceover.py --mix --music=<scored film>   # narration over the ducked music

--estimate writes timing.json from word counts, without speaking (to work on the picture).
Kokoro's model files go in out/models (see film/README.md).
"""
import argparse, hashlib, json, os, re, subprocess, sys

HERE = os.path.dirname(os.path.abspath(__file__))
OUT = os.path.join(HERE, "out")
MODELS = os.path.join(OUT, "models")

# People speed through the routine parts and slow down only for the point. Each line has
# a pace; inside a line, [slow]...[/slow] and [fast]...[/fast] change the speed of a
# phrase, and [pause 0.4] adds exactly that much silence. Kokoro takes no spoken-style
# directions, so emphasis is a slower phrase with a short pause before it. When unsure, let a
# sentence's weight fall at its end: slowing or pausing mid-sentence sounds stilted.
SPEED = {"quick": 0.98, "normal": 0.9, "land": 0.82}
PHRASE = {"slow": 0.85, "fast": 1.15}
# How words are said: the text below is what's shown in the storyboard; this is what Kokoro reads.
SPOKEN = [
    ("Saturn Five", "Saturn Five"),
    ("R, Julia and Python", "R Julia and Python"),  # the comma made a long pause after "R"
    ("renv dot lock", "R-env dot lock"),
    ("renv snapshot", "R-env snapshot"),
    ("Start renv", "Start R-env"),
    ("SQL", "sequel"),
    ("DuckDB", "Duck D B"),
    ("duckdb", "duck D B"),
    ("Project dot toml", "Project dot tom-ul"),
    ("pyproject", "pie-project"),
    ("Zulip", "Zoo-lip"),
]

# Phoneme fixes, applied after Kokoro's own phonemizer: "job" gets full stress and a long
# vowel (dʒˈɑːb), which sounds emphatic every time; secondary stress and a short vowel don't.
PHONEMES = [("dʒˈɑːbz", "dʒˌɑːbz"), ("dʒˈɑːb", "dʒˌɑb")]  # "Python jobs": the weight on Python

# What is said, in order: (text, pace).
LINES = [
    ("Saturn Five runs your heavy R, Julia and Python jobs on a much bigger computer in the cloud, straight from your laptop.", "normal"),
    ("Your files and the data are in both places, so one script works on either.", "land"),
    ("To get started, ask an admin to add you. AWS sends you an email: open it, choose a password, and set up an authenticator app.", "quick"),
    ("Then install Saturn Five. It's one line in the terminal, and you only do it once.", "quick"),
    ("Each week, sign in with saturn login. Your browser opens, you approve, and that's you for seven days.", "normal"),
    ("[slow]Then, saturn connect.[/slow] [pause 0.25] [fast]The shared workspace and the satellite imagery[/fast] appear on your laptop as folders, [pause 0.15] with one of them just for you.", "normal"),
    ("The datasets are ready to query right here. Install two packages once. Then a few lines connect you, and the rest is plain SQL.", "normal"),
    ("Each query runs in AWS, under your name, and only the answer comes back.", "normal"),
    ("Try ideas on a small slice here. When you want all of India, send it as a job.", "normal"),
    ("For the heavy work, you send your script to the cloud. That's a job.", "normal"),
    ("It runs in a container: a fresh computer that sees the same workspace as your laptop.", "normal"),
    ("So the same script runs there, unchanged, and saves its results right back into your folder.", "land"),
    ("The one thing the container doesn't have is your packages. [pause 0.2] It starts empty every time, so your project carries a list.", "normal"),
    ("In R, that list is renv dot lock. Start renv once, install what you need, then run renv snapshot to write it down.", "normal"),
    ("In Julia, it's your Project dot toml; in Python, your pyproject.", "quick"),
    ("The container reads the list and installs exactly that software before your script runs.", "land"),
    ("Then send it with saturn run, and pick a size, from small to max. It's the same file, nothing changed for the cloud, and the output streams to your terminal.", "normal"),
    ("Close the laptop if you like. [pause 0.3] The job keeps going.", "land"),
    ("When it's done, Zulip tells you, along with what it cost.", "land"),
    ("The job saved its results and its chart into the workspace, so they're already in your folder.", "land"),
    ("That's Saturn Five. [pause 0.3] The full guide is one command away: [pause 0.15] saturn docs.", "normal"),
]


def timeline(d):
    """The T table of template.html from the clip lengths d. Returns (T, DUR, starts)."""
    T, vo = {}, [0.0] * len(LINES)
    say = lambda i, at: vo.__setitem__(i, at) or at
    end = lambda i, pad: vo[i] + d[i] + pad

    T["s2"] = 3.9
    say(0, T["s2"] + 0.5)
    T["rack"] = T["s2"] + 0.7; T["pills"] = T["s2"] + 1.6
    say(1, end(0, 0.35)); T["links"] = vo[1] + 0.3
    T["s3"] = end(1, 0.7)

    say(2, T["s3"] + 0.4)
    T["side3a"] = vo[2]; T["side3b"] = vo[2] + 0.36 * d[2]; T["side3c"] = vo[2] + 0.62 * d[2]
    T["accept"] = vo[2] + 0.44 * d[2]
    T["s4"] = end(2, 0.6)

    say(3, T["s4"] + 0.3)
    T["installPaste"] = T["s4"] + 0.7; T["installOut"] = T["s4"] + 1.4
    T["s5"] = max(end(3, 0.5), T["installOut"] + 3.4)

    say(4, T["s5"] + 0.3)
    T["loginPaste"] = T["s5"] + 0.6; T["browserIn"] = T["s5"] + 1.4
    T["allow"] = max(T["browserIn"] + 1.3, vo[4] + 0.62 * d[4]); T["browserOut"] = T["allow"] + 0.7
    T["signedIn"] = T["browserOut"] + 0.4
    T["s6"] = max(end(4, 0.6), T["signedIn"] + 1.6)

    say(5, T["s6"] + 0.3)
    T["connectPaste"] = T["s6"] + 0.6; T["connectOut"] = T["s6"] + 1.2
    T["finderIn"] = vo[5] + 0.42 * d[5]; T["openWs"] = vo[5] + 0.66 * d[5]; T["mine"] = vo[5] + 0.86 * d[5]
    T["s7"] = end(5, 0.8)

    say(6, T["s7"] + 0.3)
    T["installDuck"] = vo[6] + 0.36 * d[6]; T["editorIn"] = vo[6] + 0.6 * d[6]; T["hlConnect"] = vo[6] + 0.72 * d[6]
    say(7, end(6, 0.3)); T["hlWhere"] = vo[7] + 0.1; T["result"] = vo[7] + 0.62 * d[7]
    say(8, end(7, 0.35)); T["tabJl"] = vo[8] + 0.2; T["tabPy"] = vo[8] + 0.2 + 0.3 * d[8]; T["tabR"] = vo[8] + 0.2 + 0.6 * d[8]
    T["s8"] = end(8, 0.7)

    say(9, T["s8"] + 0.4); T["boxIn"] = vo[9] + 0.55 * d[9]
    say(10, end(9, 0.3)); T["band"] = vo[10] + 0.5 * d[10]
    say(11, end(10, 0.3)); T["ghost"] = vo[11] + 0.55 * d[11]
    T["s9"] = end(11, 0.8)

    say(12, T["s9"] + 0.3); T["empty"] = vo[12] + 0.35 * d[12]
    say(13, end(12, 0.3))
    T["cInit"] = vo[13] + 0.36 * d[13]; T["cInstall"] = vo[13] + 0.52 * d[13]; T["cSnap"] = vo[13] + 0.74 * d[13]
    T["lock"] = vo[13] + d[13] - 0.2
    say(14, end(13, 0.3)); T["langJl"] = vo[14] + 0.1; T["langPy"] = vo[14] + 0.55 * d[14]
    say(15, end(14, 0.3)); T["fly"] = vo[15] + 0.2; T["lit"] = vo[15] + 1.6
    T["s10"] = end(15, 0.8)

    say(16, T["s10"] + 0.3)
    T["runPaste"] = T["s10"] + 0.7; T["sizeTag"] = vo[16] + 0.3 * d[16]; T["runOut"] = T["s10"] + 1.6
    say(17, end(16, 0.3)); T["lid"] = vo[17] + 0.4
    T["later"] = end(17, 0.5); T["lidOpen"] = T["later"] + 1.3; T["done"] = T["lidOpen"] + 0.5
    T["s11"] = T["done"] + 2.4

    T["notice"] = T["s11"] + 0.6
    say(18, T["notice"] + 0.3)
    T["s12"] = end(18, 1.0)

    say(19, T["s12"] + 0.4); T["png"] = T["s12"] + 0.6
    T["ql"] = vo[19] + 0.5 * d[19]
    T["s13"] = max(end(19, 1.6), T["ql"] + 3.0)

    say(20, T["s13"] + 0.9)
    DUR = max(end(20, 1.8), T["s13"] + 5.0)
    T = {k: round(v, 2) for k, v in T.items()}
    return T, round(DUR, 1), [round(v, 2) for v in vo]


def spoken(text):
    for a, b in SPOKEN:
        text = text.replace(a, b)
    return text


def plain(text):
    """The line without its markup: for word counts and the report."""
    return re.sub(r"\s+", " ", re.sub(r"\[(pause [\d.]+|/?slow|/?fast)\]", " ", text)).strip()


def segments(text, base):
    """[(text, speed) or (None, seconds of silence)] from a line's markup."""
    out, speed = [], base
    for tok in re.split(r"(\[pause [\d.]+\]|\[/?slow\]|\[/?fast\])", text):
        m = re.fullmatch(r"\[pause ([\d.]+)\]", tok)
        if m:
            out.append((None, float(m.group(1))))
        elif tok in ("[slow]", "[fast]"):
            speed = base * PHRASE[tok[1:-1]]
        elif tok in ("[/slow]", "[/fast]"):
            speed = base
        elif tok.strip():
            out.append((tok.strip(), speed))
    return out


def dur(path):
    return float(subprocess.run(["ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "csv=p=0", path],
                                capture_output=True, text=True, check=True).stdout.strip())


def speak(kokoro, text, pace, voice, wav):
    import numpy as np, soundfile as sf
    audio, sr = [], 24000
    for k, (p, s) in enumerate(segments(text, SPEED[pace])):
        if p is None:
            audio.append(np.zeros(int(s * sr), dtype=np.float32))
            continue
        ph = kokoro.tokenizer.phonemize(spoken(p), "en-us")
        for x, y in PHONEMES:
            ph = ph.replace(x, y)
        a, sr = kokoro.create(ph, voice=voice, speed=s, lang="en-us", is_phonemes=True)
        # Kokoro trims each phrase tightly; a breath between phrases keeps them from running together.
        if audio:
            audio.append(np.zeros(int(0.06 * sr), dtype=np.float32))
        audio.append(a.astype(np.float32))
    raw = wav + ".raw.wav"
    sf.write(raw, np.concatenate(audio), sr)
    trim = ("silenceremove=start_periods=1:start_threshold=-45dB:start_silence=0.06,areverse,"
            "silenceremove=start_periods=1:start_threshold=-45dB:start_silence=0.12,areverse")
    subprocess.run(["ffmpeg", "-y", "-v", "error", "-i", raw, "-af", trim, "-ar", "48000", wav], check=True)
    os.unlink(raw)


# Chatterbox (--engine=chatterbox) speaks faster than people talk and has no speed setting,
# so each line is stretched to its pace's words per minute (pitch kept, with Rubber Band).
TARGET_WPM = {"quick": 178, "normal": 162, "land": 142}
TRIM = ("silenceremove=start_periods=1:start_threshold=-45dB:start_silence=0.06,areverse,"
        "silenceremove=start_periods=1:start_threshold=-45dB:start_silence=0.12,areverse")


# Chatterbox runs some words together; a comma keeps them apart ("year and state" came out "urine state").
CB_SPOKEN = [("year and state", "year, and state")]


def cb(text):
    text = spoken(text)
    for x, y in CB_SPOKEN:
        text = text.replace(x, y)
    return text


def chatterbox_lines(vdir, redo):
    """Every line spoken by Chatterbox: phrases generated once (cached), then stretched and joined."""
    import numpy as np, soundfile as sf
    raw = os.path.join(vdir, "raw")
    os.makedirs(raw, exist_ok=True)
    key = lambda p: hashlib.md5(("cb1" + cb(p)).encode()).hexdigest()[:10]
    todo, seen = [], set()
    for text, _ in LINES:
        for p, _ in segments(text, 1.0):
            if p and key(p) not in seen and (redo or not os.path.exists(os.path.join(raw, key(p) + ".wav"))):
                seen.add(key(p))
                todo.append({"text": cb(p), "out": os.path.join(raw, key(p) + ".wav"), "seed": int(key(p), 16) % 2**31})
    if todo:
        jobs = os.path.join(raw, "jobs.json")
        json.dump(todo, open(jobs, "w"))
        subprocess.run(["uv", "run", "--quiet", os.path.join(HERE, "speak_chatterbox.py"), jobs], check=True)
    wavs = []
    for i, (text, pace) in enumerate(LINES):
        segs = segments(text, 1.0)
        trimmed = {}
        for p, _ in segs:
            if p:
                t = os.path.join(raw, key(p) + "-trim.wav")
                if not os.path.exists(t):
                    subprocess.run(["ffmpeg", "-y", "-v", "error", "-i", os.path.join(raw, key(p) + ".wav"), "-af", TRIM, "-ar", "48000", t], check=True)
                trimmed[p] = t
        natural = len(plain(text).split()) / sum(dur(t) for t in trimmed.values()) * 60
        ratio = min(1.6, max(1.0, natural / TARGET_WPM[pace]))
        wav = os.path.join(vdir, "%02d-%s.wav" % (i, hashlib.md5(("%s|%.3f|%r" % (text, ratio, PHRASE)).encode()).hexdigest()[:8]))
        wavs.append(wav)
        if os.path.exists(wav) and not redo:
            continue
        audio = []
        for p, s in segs:
            if p is None:
                audio.append(np.zeros(int(s * 48000), dtype=np.float32))
                continue
            stretched = wav + ".part.wav"
            subprocess.run(["rubberband", "-q", "--fine", "-t", "%.4f" % (ratio / s), trimmed[p], stretched], check=True)
            a, _ = sf.read(stretched, dtype="float32")
            os.unlink(stretched)
            if audio:
                audio.append(np.zeros(int(0.08 * 48000), dtype=np.float32))
            audio.append(a if a.ndim == 1 else a.mean(axis=1))
        sf.write(wav, np.concatenate(audio), 48000)
    return wavs


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--engine", default="kokoro", choices=["kokoro", "chatterbox"])
    ap.add_argument("--voice", default="af_heart", help="Kokoro's voice")
    ap.add_argument("--estimate", action="store_true", help="timing from word counts, no speech")
    ap.add_argument("--check", action="store_true", help="Whisper reads each clip back")
    ap.add_argument("--redo", action="store_true", help="regenerate cached clips")
    ap.add_argument("--mix", action="store_true", help="mix the narration onto the film, over the ducked music")
    ap.add_argument("--video", default=os.path.join(OUT, "saturn-v.mp4"), help="silent master")
    ap.add_argument("--music", default=os.path.join(OUT, "saturn-v-scored.mp4"), help="the film with music; its track is the bed")
    ap.add_argument("--out", default=None)
    a = ap.parse_args()
    os.makedirs(os.path.join(OUT, "vo"), exist_ok=True)

    if a.estimate:
        d = [len(plain(t).split()) / (2.75 * SPEED[p]) for t, p in LINES]
        wavs = None
    elif a.engine == "chatterbox":
        a.voice = "chatterbox"
        vdir = os.path.join(OUT, "vo", "chatterbox")
        os.makedirs(vdir, exist_ok=True)
        wavs = chatterbox_lines(vdir, a.redo)
        d = [dur(w) for w in wavs]
    else:
        from kokoro_onnx import Kokoro
        kokoro = Kokoro(os.path.join(MODELS, "kokoro-v1.0.onnx"), os.path.join(MODELS, "voices-v1.0.bin"))
        vdir = os.path.join(OUT, "vo", a.voice)
        os.makedirs(vdir, exist_ok=True)
        wavs = []
        for i, (text, pace) in enumerate(LINES):
            wav = os.path.join(vdir, "%02d-%s.wav" % (i, hashlib.md5((repr(SPEED[pace]) + repr(PHRASE) + repr(PHONEMES) + spoken(text)).encode()).hexdigest()[:8]))
            if a.redo or not os.path.exists(wav):
                speak(kokoro, text, pace, a.voice, wav)
            wavs.append(wav)
        d = [dur(w) for w in wavs]

    T, DUR, starts = timeline(d)
    json.dump({"voice": None if a.estimate else a.voice, "T": T, "DUR": DUR, "starts": starts, "durations": [round(x, 2) for x in d]},
              open(os.path.join(OUT, "vo", "timing.json"), "w"), indent=1)
    for i, (text, pace) in enumerate(LINES):
        print("%6.1f  %5.2f  %3.0f wpm  %-6s %s" % (starts[i], d[i], 60 * len(plain(text).split()) / d[i], pace, plain(text)[:70]))
    words = sum(len(plain(t).split()) for t, _ in LINES)
    print("%d lines, %d words, %.1f s of speech = %.0f wpm; film %.1f s -> out/vo/timing.json" % (len(LINES), words, sum(d), 60 * words / sum(d), DUR))

    if a.check and wavs:
        from faster_whisper import WhisperModel
        model = WhisperModel("small.en", compute_type="int8")
        import numpy as np
        for i, w in enumerate(wavs):
            pcm = subprocess.run(["ffmpeg", "-v", "error", "-i", w, "-f", "f32le", "-ac", "1", "-ar", "16000", "-"],
                                 capture_output=True, check=True).stdout
            segs, _ = model.transcribe(np.frombuffer(pcm, np.float32), beam_size=5)
            print("[%02d] %s" % (i, " ".join(s.text.strip() for s in segs)))

    if not a.mix:
        return
    total = dur(a.video)
    if abs(total - DUR) > 0.2:
        sys.exit("the master is %.1f s but this timeline is %.1f s: rebuild and export first" % (total, DUR))
    inputs, parts = [], []
    for i, (t0, wav) in enumerate(zip(starts, wavs)):
        inputs += ["-i", wav]
        parts.append("[%d:a]adelay=%d|%d,apad=whole_dur=%.2f[v%d]" % (i + 2, int(t0 * 1000), int(t0 * 1000), total, i))
    n = len(wavs)
    graph = ";".join(parts) + ";" + "".join("[v%d]" % i for i in range(n)) + "amix=inputs=%d:normalize=0,loudnorm=I=-17:TP=-2:LRA=9[vo]" % n
    # The music plays over the open card and the close card only: it fades out as the first
    # line starts, stays silent through the middle, and returns for the close and its ending.
    out0, out1 = max(0.5, starts[0] - 0.6), starts[0] + 1.2
    in0, in1 = T["s13"] - 0.6, T["s13"] + 0.8
    env = "if(lt(t,%.2f),1,if(lt(t,%.2f),(%.2f-t)/%.2f,if(lt(t,%.2f),0,if(lt(t,%.2f),(t-%.2f)/%.2f,1))))" % (
        out0, out1, out1, out1 - out0, in0, in1, in0, in1 - in0)
    graph += ";[1:a]volume=-4dB,volume='%s':eval=frame[m];" % env
    graph += "[vo]asplit[vo1][vo2];[m][vo2]sidechaincompress=threshold=0.03:ratio=5:attack=40:release=700:makeup=1[md]"
    graph += ";[md][vo1]amix=inputs=2:normalize=0,alimiter=limit=0.84:attack=5:release=60:level=false[out]"
    dest = a.out or os.path.join(OUT, "saturn-v-narrated.mp4")
    subprocess.run(["ffmpeg", "-y", "-v", "error", "-i", a.video, "-i", a.music] + inputs +
                   ["-filter_complex", graph, "-map", "0:v:0", "-map", "[out]", "-c:v", "copy", "-c:a", "aac", "-b:a", "192k",
                    "-ar", "48000", "-movflags", "+faststart", "-shortest", dest], check=True)
    print("wrote", dest, "%.1f MB" % (os.path.getsize(dest) / 1e6))


if __name__ == "__main__":
    main()
