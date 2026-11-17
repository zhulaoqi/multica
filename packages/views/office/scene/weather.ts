import type { Container, Graphics } from "pixi.js";
import type { SeasonTheme } from "../game/season";

/**
 * Gentle screen-space weather: petals, fireflies, leaves, or snow. It sits
 * above the room but below HUD overlays and never reacts to input.
 */

const PARTICLE_COUNT = 22;

interface Particle {
  view: Graphics;
  x: number;
  y: number;
  vy: number;
  drift: number;
  phase: number;
  spin: number;
}

export class OfficeWeather {
  private readonly pixi: typeof import("pixi.js");
  private readonly layer: Container;
  private theme: SeasonTheme;
  private particles: Particle[] = [];
  private width = 1;
  private height = 1;
  private elapsed = 0;

  constructor(
    pixi: typeof import("pixi.js"),
    stage: Container,
    theme: SeasonTheme,
    width: number,
    height: number,
  ) {
    this.pixi = pixi;
    this.theme = theme;
    this.layer = new pixi.Container();
    this.layer.eventMode = "none";
    stage.addChild(this.layer);
    this.resize(width, height);
    this.rebuild();
  }

  setTheme(theme: SeasonTheme) {
    if (theme.id === this.theme.id) return;
    this.theme = theme;
    this.rebuild();
  }

  resize(width: number, height: number) {
    this.width = Math.max(1, width);
    this.height = Math.max(1, height);
  }

  update(dt: number) {
    this.elapsed += dt;
    for (const particle of this.particles) {
      if (this.theme.particle === "firefly") {
        particle.phase += dt * 1.6;
        particle.view.x = particle.x + Math.sin(particle.phase) * 18;
        particle.view.y = particle.y + Math.cos(particle.phase * 0.7) * 12;
        particle.view.alpha = 0.35 + (Math.sin(particle.phase * 2) + 1) * 0.25;
        continue;
      }
      particle.y += particle.vy * dt;
      particle.x += Math.sin(this.elapsed + particle.phase) * particle.drift * dt;
      particle.view.x = particle.x;
      particle.view.y = particle.y;
      particle.view.rotation += particle.spin * dt;
      if (particle.y > this.height + 6) {
        particle.y = -6;
        particle.x = Math.random() * this.width;
      }
      if (particle.x < -6) particle.x = this.width + 6;
      if (particle.x > this.width + 6) particle.x = -6;
    }
  }

  destroy() {
    this.layer.destroy({ children: true });
  }

  private rebuild() {
    this.layer.removeChildren().forEach((child) => child.destroy());
    this.particles = [];
    for (let index = 0; index < PARTICLE_COUNT; index++) {
      const view = this.drawParticle();
      view.alpha = this.theme.particle === "firefly" ? 0.5 : 0.55;
      this.layer.addChild(view);
      this.particles.push({
        view,
        x: Math.random() * this.width,
        y: Math.random() * this.height,
        vy: this.fallSpeed(),
        drift: 6 + Math.random() * 14,
        phase: Math.random() * Math.PI * 2,
        spin: -1.2 + Math.random() * 2.4,
      });
    }
  }

  private fallSpeed(): number {
    if (this.theme.particle === "snow") return 10 + Math.random() * 16;
    if (this.theme.particle === "leaf") return 18 + Math.random() * 22;
    if (this.theme.particle === "petal") return 12 + Math.random() * 14;
    return 0;
  }

  private drawParticle(): Graphics {
    const view = new this.pixi.Graphics();
    const color = this.theme.tint;
    switch (this.theme.particle) {
      case "snow":
        view.rect(-1, -1, 2, 2).fill({ color, alpha: 0.9 });
        break;
      case "leaf":
        view.rect(-2, -1, 4, 2).fill(color);
        view.rect(-1, -2, 2, 1).fill({ color: 0x8f5a32, alpha: 0.7 });
        break;
      case "petal":
        view.rect(-1.5, -1, 3, 2).fill(color);
        break;
      case "firefly":
        view.circle(0, 0, 1.6).fill(color);
        view.circle(0, 0, 3).fill({ color, alpha: 0.22 });
        break;
    }
    return view;
  }
}
