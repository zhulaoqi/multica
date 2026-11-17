/**
 * Synthesised office audio. There are no sound assets to download: a quiet
 * two-oscillator pad runs while sound is on, and short triangle notes mark
 * events, sign-offs, and celebrations. Browsers only allow audio after a
 * gesture, so the page constructs this only when the user turns sound on.
 */

export type OfficeCue = "event" | "celebrate" | "sign" | "meow";

const AMBIENT_GAIN = 0.012;
const CUE_GAIN = 0.05;

export class OfficeAudio {
  private ctx: AudioContext | null = null;
  private master: GainNode | null = null;
  private ambient: OscillatorNode[] = [];

  get running(): boolean {
    return this.ctx !== null;
  }

  start(): boolean {
    if (this.ctx) return true;
    if (typeof window === "undefined") return false;
    const Ctor =
      window.AudioContext ??
      (window as unknown as { webkitAudioContext?: typeof AudioContext })
        .webkitAudioContext;
    if (!Ctor) return false;
    try {
      const ctx = new Ctor();
      const master = ctx.createGain();
      master.gain.value = 1;
      master.connect(ctx.destination);
      this.ctx = ctx;
      this.master = master;
      void ctx.resume?.();

      const pad = ctx.createGain();
      pad.gain.value = AMBIENT_GAIN;
      const filter = ctx.createBiquadFilter();
      filter.type = "lowpass";
      filter.frequency.value = 620;
      pad.connect(filter);
      filter.connect(master);
      for (const [frequency, detune] of [
        [110, -4],
        [164.81, 3],
      ] as const) {
        const oscillator = ctx.createOscillator();
        oscillator.type = "sine";
        oscillator.frequency.value = frequency;
        oscillator.detune.value = detune;
        oscillator.connect(pad);
        oscillator.start();
        this.ambient.push(oscillator);
      }
      return true;
    } catch {
      this.ctx = null;
      this.master = null;
      return false;
    }
  }

  stop() {
    for (const oscillator of this.ambient) {
      try {
        oscillator.stop();
      } catch {
        // Already stopped with its context.
      }
    }
    this.ambient = [];
    const ctx = this.ctx;
    this.ctx = null;
    this.master = null;
    void ctx?.close().catch(() => {});
  }

  cue(kind: OfficeCue) {
    const ctx = this.ctx;
    const master = this.master;
    if (!ctx || !master) return;
    const notes: Record<OfficeCue, readonly number[]> = {
      event: [523.25, 659.25],
      celebrate: [523.25, 659.25, 783.99],
      sign: [440, 587.33],
      meow: [392, 329.63],
    };
    const start = ctx.currentTime;
    notes[kind].forEach((frequency, index) => {
      const oscillator = ctx.createOscillator();
      const gain = ctx.createGain();
      oscillator.type = "triangle";
      oscillator.frequency.value = frequency;
      const at = start + index * 0.09;
      gain.gain.setValueAtTime(0, at);
      gain.gain.linearRampToValueAtTime(CUE_GAIN, at + 0.02);
      gain.gain.exponentialRampToValueAtTime(0.0001, at + 0.32);
      oscillator.connect(gain);
      gain.connect(master);
      oscillator.start(at);
      oscillator.stop(at + 0.36);
    });
  }
}
