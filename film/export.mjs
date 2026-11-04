// Renders the film to MP4 through Chrome's DevTools protocol: one PNG screenshot per frame
// with window.ad.step(t), stitched by ffmpeg (the AccioPix exporter, trimmed to one film).
//   node film/build.mjs && node film/export.mjs [--width=1920] [--crf=16] [--fps=30] [--from=0 --to=<s>]
// Needs Chrome (CHROME env or the macOS default) and ffmpeg. Writes out/saturn-v[-<width>].mp4
// and out/saturn-v-poster.jpg. The film is silent here; voiceover.py --mix adds narration and music.
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { spawn, execFileSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const OUT = path.join(HERE, 'out');
const PAGE = path.join(OUT, 'saturn-v.html');
const CHROME = process.env.CHROME || '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome';
const arg = (name, fallback) => { const hit = process.argv.find((a) => a.startsWith(`--${name}=`)); return hit ? hit.slice(name.length + 3) : fallback; };
const FPS = +arg('fps', 30);
const WIDTH = +arg('width', 1920);
const CRF = arg('crf', '16');
const VIEW_H = 900;
const PORT = 9400 + Math.floor(Math.random() * 100);
if (!fs.existsSync(PAGE)) { console.error('Build first: node film/build.mjs'); process.exit(1); }

// ---- Chrome + DevTools -----------------------------------------------------
const profile = fs.mkdtempSync(path.join(os.tmpdir(), 'film-chrome-'));
const chrome = spawn(CHROME, ['--headless=new', '--disable-gpu', '--hide-scrollbars', '--allow-file-access-from-files',
  `--remote-debugging-port=${PORT}`, `--user-data-dir=${profile}`, `--window-size=1400,${VIEW_H}`, 'about:blank'], { stdio: 'ignore' });
let cleaned = false;
const cleanup = () => {
  if (cleaned) return;
  cleaned = true;
  try { chrome.kill(); } catch { /* gone */ }
  // Chrome keeps writing to its profile for a moment after SIGTERM.
  try { fs.rmSync(profile, { recursive: true, force: true, maxRetries: 10, retryDelay: 200 }); } catch { /* temp dir, leave it */ }
};
process.on('exit', cleanup);

async function pageTarget() {
  for (let i = 0; i < 80; i++) {
    try {
      const list = await (await fetch(`http://127.0.0.1:${PORT}/json/list`)).json();
      const page = list.find((t) => t.type === 'page');
      if (page) return page.webSocketDebuggerUrl;
    } catch { /* not up yet */ }
    await new Promise((r) => setTimeout(r, 250));
  }
  throw new Error('Chrome did not answer on the DevTools port');
}

const ws = new WebSocket(await pageTarget());
await new Promise((res, rej) => { ws.onopen = res; ws.onerror = rej; });
let nextId = 1;
const pending = new Map();
const events = new Map();
ws.onmessage = (m) => {
  const msg = JSON.parse(m.data);
  if (msg.id && pending.has(msg.id)) { const { res, rej } = pending.get(msg.id); pending.delete(msg.id); msg.error ? rej(new Error(msg.error.message)) : res(msg.result); }
  else if (msg.method && events.has(msg.method)) { events.get(msg.method)(); events.delete(msg.method); }
};
const send = (method, params = {}) => new Promise((res, rej) => { const id = nextId++; pending.set(id, { res, rej }); ws.send(JSON.stringify({ id, method, params })); });
const once = (method) => new Promise((res) => events.set(method, res));
const evaluate = async (expression, awaitPromise = false) => (await send('Runtime.evaluate', { expression, awaitPromise, returnByValue: true })).result.value;

await send('Page.enable');
await send('Emulation.setDeviceMetricsOverride', { width: 1400, height: VIEW_H, deviceScaleFactor: 1, mobile: false });

// ---- frames --------------------------------------------------------------
const loaded = once('Page.loadEventFired');
await send('Page.navigate', { url: `file://${PAGE}?still=0` });
await loaded;
await evaluate('document.fonts.ready.then(() => true)', true);
await new Promise((r) => setTimeout(r, 500)); // the chart decodes
// The page shows the stage as a rounded card; the film wants square corners and nothing behind them.
await evaluate("(function () { var w = document.getElementById('stageWrap'); w.style.borderRadius = '0'; w.style.boxShadow = 'none'; document.body.style.background = '#ffffff'; })()");
const rect = JSON.parse(await evaluate('JSON.stringify(document.getElementById("stageWrap").getBoundingClientRect())'));
const clip = { x: rect.x, y: rect.y, width: rect.width, height: rect.height, scale: WIDTH / rect.width };
const duration = await evaluate('window.ad.duration');
// --stills=1.5,12,40 renders those moments to out/stills/ (for a contact sheet) and stops.
if (arg('stills', '')) {
  const sd = path.join(OUT, 'stills');
  fs.rmSync(sd, { recursive: true, force: true }); fs.mkdirSync(sd, { recursive: true });
  for (const s of arg('stills', '').split(',').map(Number)) {
    await evaluate(`window.ad.drawAt(${s})`);
    const shot = await send('Page.captureScreenshot', { format: 'png', clip: { ...clip, scale: 960 / rect.width } });
    fs.writeFileSync(path.join(sd, `s${String(Math.round(s * 10)).padStart(5, '0')}.png`), Buffer.from(shot.data, 'base64'));
  }
  console.log(`  stills in ${path.relative(process.cwd(), sd)}`);
  ws.close(); cleanup(); process.exit(0);
}
const from = +arg('from', 0), to = +arg('to', duration);
const frames = Math.round((to - from) * FPS);
const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'film-frames-'));
const t0 = Date.now();
for (let i = 0; i < frames; i++) {
  await evaluate(`window.ad.step(${(from + i / FPS).toFixed(4)})`);
  const shot = await send('Page.captureScreenshot', { format: 'png', clip });
  fs.writeFileSync(path.join(dir, `f${String(i).padStart(5, '0')}.png`), Buffer.from(shot.data, 'base64'));
  if (i % 150 === 0) console.log(`  frame ${i}/${frames} (${Math.round((Date.now() - t0) / 1000)} s)`);
  if (i === Math.round(1.6 * FPS) && from === 0) execFileSync('ffmpeg', ['-y', '-loglevel', 'error', '-i', path.join(dir, `f${String(i).padStart(5, '0')}.png`), '-q:v', '3', path.join(OUT, 'saturn-v-poster.jpg')]);
}
console.log(`  ${frames} frames in ${Math.round((Date.now() - t0) / 1000)} s`);
const out = path.join(OUT, `saturn-v${WIDTH === 1920 ? '' : '-' + WIDTH}.mp4`);
execFileSync('ffmpeg', ['-y', '-loglevel', 'error', '-framerate', String(FPS), '-i', path.join(dir, 'f%05d.png'),
  '-c:v', 'libx264', '-preset', 'slow', '-crf', CRF, '-profile:v', 'high', '-pix_fmt', 'yuv420p', '-movflags', '+faststart',
  '-vf', `scale=${WIDTH}:-2:flags=lanczos`, out]);
fs.rmSync(dir, { recursive: true, force: true });
console.log(`  ${path.relative(process.cwd(), out)}: ${Math.round(fs.statSync(out).size / 1024)} KB`);
ws.close();
cleanup();
process.exit(0);
