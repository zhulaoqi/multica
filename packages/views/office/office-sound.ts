/**
 * Synthesised office audio. There are no sound assets to download: a warm,
 * slow-breathing pad runs while sound is on, and short musical phrases mark
 * real milestones (work completed, a level-up, a sign-off, a failure).
 *
 * The floor is deliberately quiet. Random office events and manual actions
 * have no sound; anything that rings is tied to work the user cares about.
 * Browsers only allow audio after a gesture, so the page creates this only
 * when the user turns sound on (or replays a saved preference on gesture).
 */

export type OfficeCue = "complete" | "level" | "sign" | "alert";

type SoundState = "off" | "starting" | "on" | "suspended" | "closing";

const AMBIENT_GAIN = 0.006;
const AMBIENT_DEPTH = 0.002;
const CUE_GAIN = 0.04;
const CUE_MIN_GAP_MS = 1_500;
const FADE_S = 0.5;
const CLOSE_DELAY_MS = 600;

const AMBIENT_FREQUENCIES = [130.81, 196.0, 329.63] as const; // C3, G3, E4

const CUES: Record<OfficeCue, readonly number[]> = {
  complete: [523.25, 659.25], // C5 -> E5
  level: [523.25, 659.25, 783.99, 1046.5], // C5 -> E5 -> G5 -> C6
  sign: [392, 587.33], // G4 -> D5
  alert: [220, 174.61], // A3 -> F3
};

export class OfficeAudio {
  private ctx: AudioContext | null = null;
  private master: GainNode | null = null;
  private ambient: OscillatorNode[] = [];
  private cues = new Set<OscillatorNode>();
  private state: SoundState = "off";
  /** Guards delayed close/fade work against a fresh context. */
  private generation = 0;
  private lastCueAt = -Infinity;

  get running(): boolean {
    return this.state === "on" || this.state === "suspended";
  }

  start(): boolean {
    if (this.state === "on") return true;
    if (this.state === "suspended" && this.ctx) {
      this.resume();
      return true;
    }
    if (typeof window === "undefined") return false;
    const Ctor =
      window.AudioContext ??
      (window as unknown as { webkitAudioContext?: typeof AudioContext })
        .webkitAudioContext;
    if (!Ctor) return false;

    this.state = "starting";
    this.generation += 1;
    try {
      const ctx = new Ctor();
      const master = ctx.createGain();
      master.gain.setValueAtTime(0, ctx.currentTime);
      master.gain.linearRampToValueAtTime(1, ctx.currentTime + FADE_S);
      master.connect(ctx.destination);
      this.ctx = ctx;
      this.master = master;
      this.lastCueAt = -Infinity;
      void ctx.resume?.().catch(() => {});

      this.startAmbient(ctx, master);
      this.state = "on";
      return true;
    } catch {
      this.teardown();
      return false;
    }
  }

  stop() {
    const ctx = this.ctx;
    const master = this.master;
    if (!ctx || !master) {
      this.teardown();
      return;
    }
    const generation = ++this.generation;
    this.state = "closing";
    // Detach immediately so a new start can create a fresh context while the
    // old one fades out underneath the same generation guard.
    this.ctx = null;
    this.master = null;
    try {
      const now = ctx.currentTime;
      master.gain.cancelScheduledValues(now);
      master.gain.setValueAtTime(master.gain.value, now);
      master.gain.linearRampToValueAtTime(0.0001, now + FADE_S);
      this.stopAmbient(now + FADE_S);
      this.stopCues(now + FADE_S);
    } catch {
      // The context may already be closed.
    }
    globalThis.setTimeout(() => {
      void ctx.close().catch(() => {});
      if (this.generation === generation && this.state === "closing") {
        this.state = "off";
      }
    }, CLOSE_DELAY_MS);
  }

  setHidden(hidden: boolean) {
    if (hidden) {
      if (this.state !== "on" || !this.ctx) return;
      this.state = "suspended";
      void this.ctx.suspend().catch(() => {});
      return;
    }
    if (this.state === "suspended") this.resume();
  }

  cue(kind: OfficeCue) {
    if (this.state !== "on") return;
    const ctx = this.ctx;
    const master = this.master;
    if (!ctx || !master || ctx.state !== "running") return;

    const now = performance.now();
    if (now - this.lastCueAt < CUE_MIN_GAP_MS) return;
    this.lastCueAt = now;

    const start = ctx.currentTime + 0.01;
    for (const [index, frequency] of CUES[kind].entries()) {
      const at = start + index * 0.12;
      const oscillator = ctx.createOscillator();
      const gain = ctx.createGain();
      oscillator.type = "triangle";
      oscillator.frequency.value = frequency;
      gain.gain.setValueAtTime(0.0001, at);
      gain.gain.linearRampToValueAtTime(CUE_GAIN, at + 0.01);
      gain.gain.exponentialRampToValueAtTime(0.0001, at + 0.45);
      oscillator.connect(gain);
      gain.connect(master);
      oscillator.start(at);
      oscillator.stop(at + 0.5);
      this.cues.add(oscillator);
      oscillator.onended = () => {
        this.cues.delete(oscillator);
        try {
          oscillator.disconnect();
          gain.disconnect();
        } catch {
          // Already detached with its context.
        }
      };
    }
  }

  private resume() {
    const ctx = this.ctx;
    if (!ctx) return;
    void ctx
      .resume()
      .then(() => {
        if (this.ctx === ctx && this.state === "suspended") this.state = "on";
      })
      .catch(() => {});
  }

  private startAmbient(ctx: AudioContext, master: GainNode) {
    const pad = ctx.createGain();
    pad.gain.value = AMBIENT_GAIN;
    const filter = ctx.createBiquadFilter();
    filter.type = "lowpass";
    filter.frequency.value = 700;
    pad.connect(filter);
    filter.connect(master);

    for (const frequency of AMBIENT_FREQUENCIES) {
      const oscillator = ctx.createOscillator();
      oscillator.type = "triangle";
      oscillator.frequency.value = frequency;
      oscillator.connect(pad);
      oscillator.start();
      this.ambient.push(oscillator);
    }

    // A very slow swell keeps the pad from sounding like a stuck organ note.
    const lfo = ctx.createOscillator();
    const lfoGain = ctx.createGain();
    lfo.type = "sine";
    lfo.frequency.value = 0.03;
    lfoGain.gain.value = AMBIENT_DEPTH;
    lfo.connect(lfoGain);
    lfoGain.connect(pad.gain);
    lfo.start();
    this.ambient.push(lfo);
  }

  private stopAmbient(at?: number) {
    for (const oscillator of this.ambient) {
      try {
        if (at === undefined) {
          oscillator.stop();
          oscillator.disconnect();
        } else {
          // Let the master fade finish before the sources go quiet.
          oscillator.stop(at);
        }
      } catch {
        // Already stopped with its context.
      }
    }
    this.ambient = [];
  }

  private stopCues(at?: number) {
    for (const oscillator of this.cues) {
      try {
        if (at === undefined) {
          oscillator.stop();
          oscillator.disconnect();
        } else {
          oscillator.stop(at);
        }
      } catch {
        // Already stopped with its context.
      }
    }
    this.cues.clear();
  }

  private teardown() {
    const ctx = this.ctx;
    this.ctx = null;
    this.master = null;
    this.state = "off";
    this.stopAmbient();
    this.stopCues();
    if (ctx) void ctx.close().catch(() => {});
  }
}
