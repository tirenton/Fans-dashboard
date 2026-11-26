// Generates FAKE prototype output for trying the dashboard without real artist activity:
//   node api/testdata/make-demo.mjs   ->  api/testdata/demo/{captures/*.json, log.jsonl}
// Every post, nickname and comment here is invented and labelled DEMO.
import fs from 'node:fs';
import path from 'node:path';

const OUT = path.join(path.dirname(new URL(import.meta.url).pathname).replace(/^\/([A-Za-z]:)/, '$1'), 'demo');
fs.rmSync(OUT, { recursive: true, force: true });
fs.mkdirSync(path.join(OUT, 'captures'), { recursive: true });
// the demo's /config (accounts etc.): the API runs as uid 65532, so let it write here on Linux too
fs.mkdirSync(path.join(OUT, 'config'));
fs.chmodSync(path.join(OUT, 'config'), 0o777);

let seed = 7;
const rnd = () => ((seed = (seed * 16807) % 2147483647) / 2147483647);
const pick = (a) => a[Math.floor(rnd() * a.length)];
const MEMBERS = ['LILY', 'HAEWON', 'SULLYOON', 'BAE', 'JIWOO', 'KYUJIN'];
const WEIGHT = { LILY: 3, HAEWON: 2, SULLYOON: 2, BAE: 4, JIWOO: 1, KYUJIN: 3 };
const name = (t) => t.toISOString().replace(/[:.]/g, '-');

const end = Date.now();
let t = end - 48 * 3600e3;
const posts = []; // newest first
let nextId = 90000000;
const log = [];
let tokenExp = t + 20 * 3600e3, cookieExp = t + 60 * 3600e3;

log.push({ at: new Date(t - 60e3).toISOString(), kind: 'pass', headed: true, tokenTimes: { exp: new Date(tokenExp).toISOString() },
  probe: { webdriver: true }, capture: { posts: 10 }, apiRequests: { total: 18, statuses: { 200: 18 }, refresh: [] } });

while (t < end) {
  // new fan posts since the last pass
  const n = Math.floor(rnd() * 4);
  for (let i = 0; i < n; i++) {
    posts.unshift({ id: String(nextId++), created: t - rnd() * 600e3, likes: new Set(), comment: null });
  }
  // artists are more active in the evening, Korea time (UTC+9)
  const kstHour = (new Date(t).getUTCHours() + 9) % 24;
  const active = kstHour >= 19 || kstHour <= 1 ? 0.35 : kstHour >= 12 ? 0.12 : 0.03;
  for (const p of posts.slice(0, 10)) {
    for (const m of MEMBERS) if (rnd() < active * WEIGHT[m] / 30) p.likes.add(m);
    if (rnd() < active / 25) p.comment = { m: pick(MEMBERS), at: t - rnd() * 500e3 };
  }

  const refresh = t > tokenExp - 170e3;
  const type = refresh ? 'refresh-load' : 'light';
  if (refresh) { tokenExp = t + 24 * 3600e3; cookieExp = t + 72 * 3600e3; }
  const objs = posts.slice(0, 10).map((p) => ({
    id: p.id, slug: `demo${p.id}`, body: `DEMO post ${p.id}: an invented fan message for testing the dashboard.`,
    firstActivatedAt: new Date(p.created).toISOString(), likeCount: 3, commentCount: 1,
    member: { nickname: `demo_fan_${Number(p.id) % 97}`, artist: null },
    likedArtists: [...p.likes].map((m) => ({ member: { nickname: m, artist: { code: m.toLowerCase() } } })),
    lastArtistComment: p.comment ? { body: `DEMO comment from ${p.comment.m}`, firstActivatedAt: new Date(p.comment.at).toISOString(), member: { nickname: p.comment.m } } : null,
  }));
  const file = `${name(new Date(t))}-${type}.json`;
  fs.writeFileSync(path.join(OUT, 'captures', file), JSON.stringify({ data: { posts: { objects: objs, page: { hasNext: true } } } }));
  log.push({
    at: new Date(t + 9e3).toISOString(), kind: 'pass', type,
    tokenAfter: { exp: new Date(tokenExp).toISOString() }, probe: { webdriver: true },
    capture: { file, posts: objs.length },
    apiRequests: { total: refresh ? 3 : 1, preflight: 1, statuses: { 200: refresh ? 3 : 1 }, refresh: refresh ? [{ status: 200 }] : [] },
    cookiesAfter: [{ name: 'refreshToken:web', domain: 'api.app.fans', expires: new Date(cookieExp).toISOString() }],
  });
  // a stretch of downtime, to show uncertain detections
  if (Math.abs(t - (end - 30 * 3600e3)) < 450e3) {
    t += 3 * 3600e3; // likes that happen while the probe is down
    for (const p of posts.slice(0, 10)) if (rnd() < 0.6) p.likes.add(pick(MEMBERS));
  } else t += (8 + rnd() * 7) * 60e3;
}
log.splice(3, 0, { at: log[2].at.replace(/:\d\d\./, ':59.'), kind: 'skip', type: 'refresh-load', reason: 'request budget' });
fs.writeFileSync(path.join(OUT, 'log.jsonl'), log.map((l) => JSON.stringify(l)).join('\n') + '\n');
console.log(`wrote ${fs.readdirSync(path.join(OUT, 'captures')).length} DEMO captures to ${OUT}`);
