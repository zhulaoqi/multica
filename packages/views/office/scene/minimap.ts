import type { Container, Graphics } from "pixi.js";
import { OFFICE_WORLD } from "@multica/core/office";
import type { OfficeLayout, RoomId } from "./layout";
import { STATUS_COLORS, type AgentDeskStatus } from "./characters";

/**
 * Screen-space floor plan in the corner: rooms, the player, agents by status,
 * and teammates. It is a read-only overview, so it ignores pointer events.
 */

const MAP_WIDTH = 132;
const MAP_HEIGHT = Math.round((MAP_WIDTH * OFFICE_WORLD.height) / OFFICE_WORLD.width);
const SCALE = MAP_WIDTH / OFFICE_WORLD.width;
const PADDING = 10;
const BORDER = 0x475569;
const FILL = 0x111827;
const ROOM_FILLS: Record<RoomId, number> = {
  meeting: 0x26404f,
  kitchen: 0x4a3a22,
  server: 0x25402d,
  office: 0x233c3a,
  lounge: 0x442a2a,
  reception: 0x3a2b45,
  print: 0x30383f,
};

export interface MinimapAgent {
  x: number;
  y: number;
  status: AgentDeskStatus;
}

export class OfficeMinimap {
  private readonly view: Container;
  private readonly base: Graphics;
  private readonly dots: Graphics;
  private layout: OfficeLayout;
  private visible = false;

  constructor(
    pixi: typeof import("pixi.js"),
    stage: Container,
    layout: OfficeLayout,
  ) {
    this.layout = layout;
    this.view = new pixi.Container();
    this.view.eventMode = "none";
    this.base = new pixi.Graphics();
    this.dots = new pixi.Graphics();
    this.view.addChild(this.base, this.dots);
    this.view.visible = false;
    stage.addChild(this.view);
    this.rebuild();
  }

  setLayout(layout: OfficeLayout) {
    this.layout = layout;
    this.rebuild();
  }

  setVisible(visible: boolean) {
    this.visible = visible;
    this.view.visible = visible;
  }

  isVisible(): boolean {
    return this.visible;
  }

  resize(screenWidth: number) {
    this.view.position.set(
      Math.max(PADDING, screenWidth - MAP_WIDTH - PADDING),
      PADDING,
    );
  }

  update(
    local: { x: number; y: number },
    agents: readonly MinimapAgent[],
    peers: readonly { x: number; y: number }[],
    cat: { x: number; y: number } | null,
  ) {
    if (!this.visible) return;
    this.dots.clear();
    if (cat) this.dot(cat.x, cat.y, 0xf1a85a, 1.8);
    for (const peer of peers) this.dot(peer.x, peer.y, 0x7cc0d8, 2);
    for (const agent of agents) {
      this.dot(agent.x, agent.y, STATUS_COLORS[agent.status], 2.2);
    }
    // Ring first so the player stays legible against any room colour.
    const px = this.mapX(local.x);
    const py = this.mapY(local.y);
    this.dots.circle(px, py, 4.2).fill({ color: 0xf8fafc, alpha: 0.35 });
    this.dots.circle(px, py, 2.6).fill(0xf8fafc);
    this.dots.circle(px, py, 3.6).stroke({ color: 0x0f172a, width: 1, alpha: 0.8 });
  }

  destroy() {
    this.view.destroy({ children: true });
  }

  private dot(x: number, y: number, color: number, radius: number) {
    this.dots
      .circle(this.mapX(x), this.mapY(y), radius)
      .fill(color)
      .circle(this.mapX(x), this.mapY(y), radius + 0.8)
      .stroke({ color: 0x0f172a, width: 0.8, alpha: 0.6 });
  }

  private mapX(x: number): number {
    return Math.min(MAP_WIDTH, Math.max(0, x * SCALE));
  }

  private mapY(y: number): number {
    return Math.min(MAP_HEIGHT, Math.max(0, y * SCALE));
  }

  private rebuild() {
    this.base.clear();
    this.base
      .roundRect(-3, -3, MAP_WIDTH + 6, MAP_HEIGHT + 6, 4)
      .fill({ color: 0x0f172a, alpha: 0.82 })
      .stroke({ color: BORDER, width: 1, alpha: 0.9 });
    this.base.rect(0, 0, MAP_WIDTH, MAP_HEIGHT).fill({ color: FILL, alpha: 0.9 });
    for (const room of this.layout.rooms) {
      const x = room.rect.x * 16 * SCALE;
      const y = room.rect.y * 16 * SCALE;
      const width = room.rect.w * 16 * SCALE;
      const height = room.rect.h * 16 * SCALE;
      this.base
        .rect(x + 0.6, y + 0.6, width - 1.2, height - 1.2)
        .fill({ color: ROOM_FILLS[room.id], alpha: 0.9 });
    }
  }
}
