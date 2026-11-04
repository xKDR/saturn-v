// Builds the film page (out/saturn-v.html): template.html with the xKDR logo, the job's
// chart and the narration timeline put in, so it plays from file:// and exports with export.mjs.
//   node film/build.mjs
// Inputs: out/vo/timing.json (uv run film/voiceover.py, or --estimate), job.json (what the
// real job printed), and the chart the job drew (CHART, default the one in the workspace).
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { execFileSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const OUT = path.join(HERE, 'out');
const LOGO = path.join(HERE, 'xkdr-logo.svg');
const CHART = process.env.CHART || path.join(os.homedir(), 'aws/workspace/ayush.patnaik/income/income-by-state.png');
const TIMING = path.join(OUT, 'vo/timing.json');

if (!fs.existsSync(TIMING)) { console.error('No timeline yet: uv run film/voiceover.py (or --estimate)'); process.exit(1); }
const timing = JSON.parse(fs.readFileSync(TIMING, 'utf8'));
const job = JSON.parse(fs.readFileSync(path.join(HERE, 'job.json'), 'utf8'));

// The chart, scaled for the stage (the original is 2400 px wide).
fs.mkdirSync(OUT, { recursive: true });
const small = path.join(OUT, 'chart.png');
execFileSync('ffmpeg', ['-y', '-v', 'error', '-i', CHART, '-vf', 'scale=1600:-1:flags=lanczos', small]);
const chart = 'data:image/png;base64,' + fs.readFileSync(small).toString('base64');

const logo = fs.readFileSync(LOGO, 'utf8').trim()
  .replace(/\s(width|height)="\d+"/g, '')
  .replace('<svg ', '<svg class="logo" role="img" aria-label="XKDR" ');
const film = { T: timing.T, DUR: timing.DUR, voice: timing.voice, JOB: job, CHART: chart };
const page = fs.readFileSync(path.join(HERE, 'template.html'), 'utf8')
  .replaceAll('/*__LOGO__*/', logo)
  .replace('/*__DATA__*/', 'window.FILM = ' + JSON.stringify(film) + ';');
fs.writeFileSync(path.join(OUT, 'saturn-v.html'), page);
console.log(`out/saturn-v.html: ${Math.round(Buffer.byteLength(page) / 1024)} KB, ${timing.DUR} s, timing from ${timing.voice || 'an estimate'}`);
