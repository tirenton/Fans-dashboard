// The chat's new-message sound: "Sparkle", three quick rising notes (C6, E6, G6), synthesized with the Web Audio API
// so there's no audio file. Browsers allow sound only after the person has clicked or pressed a key on the page, so
// the audio context is made on the first such gesture; before that, playMessageSound does nothing.
// Chosen 2026-11-25 from six samples.

const PREF_KEY = "axismixx.chatSound"; // "off" mutes, in this browser only
const VOLUME = 0.45;

let ctx: AudioContext | null = null;

function unlock() {
  if (!ctx) {
    const AC = window.AudioContext ?? (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;
    if (!AC) return;
    ctx = new AC();
  }
  if (ctx.state === "suspended") void ctx.resume();
}

/** Call once on the client: makes (or wakes) the audio context on the person's first click or key press. */
export function armMessageSound() {
  const once = () => {
    unlock();
    window.removeEventListener("pointerdown", once);
    window.removeEventListener("keydown", once);
  };
  window.addEventListener("pointerdown", once);
  window.addEventListener("keydown", once);
  return once;
}

function tone(c: AudioContext, out: AudioNode, t: number, freq: number, len: number, gain: number) {
  const o = c.createOscillator(), g = c.createGain();
  o.type = "triangle";
  o.frequency.setValueAtTime(freq, t);
  g.gain.setValueAtTime(0.0001, t);
  g.gain.exponentialRampToValueAtTime(gain, t + 0.005);
  g.gain.exponentialRampToValueAtTime(0.0001, t + len);
  o.connect(g).connect(out);
  o.start(t);
  o.stop(t + len + 0.02);
}

export function playMessageSound() {
  if (!ctx || ctx.state !== "running") return; // no gesture yet, or the browser suspended it
  const master = ctx.createGain();
  master.gain.value = VOLUME;
  master.connect(ctx.destination);
  const t = ctx.currentTime + 0.02;
  [1046.5, 1318.51, 1567.98].forEach((f, i) => tone(ctx!, master, t + i * 0.07, f, 0.2, 0.35));
}

export function soundPref(): boolean {
  try { return localStorage.getItem(PREF_KEY) !== "off"; } catch { return true; }
}

export function setSoundPref(on: boolean) {
  try { localStorage.setItem(PREF_KEY, on ? "on" : "off"); } catch { /* remembered for this visit only */ }
}
