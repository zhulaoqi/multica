import type * as THREE from "three";

type ThreeModule = typeof import("three");
type Group = InstanceType<ThreeModule["Group"]>;
type Mesh = InstanceType<ThreeModule["Mesh"]>;
type Sprite = InstanceType<ThreeModule["Sprite"]>;
type InstancedMesh = InstanceType<ThreeModule["InstancedMesh"]>;
type Texture = InstanceType<ThreeModule["Texture"]>;
type Vector3 = InstanceType<ThreeModule["Vector3"]>;

/**
 * Set dressing for the sign-in backdrop's dawn port: the wet quay, container
 * yard, gantry cranes, the ship at berth, sodium lamp masts, drifting mist and
 * rain. Each builder returns the object plus an optional per-frame update.
 */

// Port-authority palette (mirrors the reference title card).
const SIGNAL = 0xffb347;
const SIGNAL_HOT = 0xffc977;
const RUST = 0xd2622a;
const ALARM = 0xff5a40;

const RAIN_COUNT = 620;
const MIST_COUNT = 22;

// Quay: the wet concrete apron the camera stands on. Low roughness keeps the
// lamp streaks and the dawn sheen alive; bollards and a rail line sell scale.
// ---------------------------------------------------------------------------

export function buildQuay(THREE: ThreeModule, roughnessMap: Texture): Group {
  const group = new THREE.Group();
  const concrete = new THREE.MeshStandardMaterial({
    color: 0x0e1318,
    roughness: 0.62,
    roughnessMap,
    metalness: 0.42,
    envMapIntensity: 1.5,
  });
  const slab = new THREE.Mesh(new THREE.BoxGeometry(260, 1.2, 76), concrete);
  slab.position.set(0, -0.6, 26);
  group.add(slab);

  // Quay edge: a lighter curb and bollard line at the water.
  const curb = new THREE.Mesh(
    new THREE.BoxGeometry(260, 0.5, 1.4),
    new THREE.MeshStandardMaterial({ color: 0x2b3138, roughness: 0.6 }),
  );
  curb.position.set(0, 0.05, -11.4);
  group.add(curb);
  const bollardMat = new THREE.MeshStandardMaterial({ color: 0x23282e, roughness: 0.55, metalness: 0.3 });
  const bollardGeo = new THREE.CylinderGeometry(0.28, 0.36, 0.85, 10);
  for (let i = 0; i < 14; i += 1) {
    const bollard = new THREE.Mesh(bollardGeo, bollardMat);
    bollard.position.set(-78 + i * 12, 0.42, -10.4);
    group.add(bollard);
  }

  // Embedded rail tracks for the gantry cranes: two dark steel lines.
  const railMat = new THREE.MeshStandardMaterial({ color: 0x30363d, roughness: 0.3, metalness: 0.75 });
  for (const x of [-26, 26]) {
    const rail = new THREE.Mesh(new THREE.BoxGeometry(2.4, 0.12, 90), railMat);
    rail.position.set(x, 0.02, 4);
    group.add(rail);
  }
  return group;
}

// ---------------------------------------------------------------------------
// Container yard: instanced boxes in the terminal's muted rust/teal/slate
// palette, stacked 1–3 high in two blocks that leave a sun-lit lane down the
// middle — the leading line of the composition.
// ---------------------------------------------------------------------------

const CONTAINER_COLORS = [0xd2622a, 0x2f5f63, 0x3a4750, 0xa5803a, 0x6e3b28, 0x44525e, 0x8c4a2f];

export function buildContainerYard(THREE: ThreeModule, random: () => number, paint: Texture): InstancedMesh {
  const geometry = new THREE.BoxGeometry(6.0, 2.55, 2.5);
  const material = new THREE.MeshStandardMaterial({
    map: paint,
    roughness: 0.58,
    metalness: 0.35,
    envMapIntensity: 0.9,
  });

  const placements: Array<{ x: number; y: number; z: number; ry: number; color: number }> = [];
  for (const side of [-1, 1]) {
    for (let row = 0; row < 4; row += 1) {
      for (let col = 0; col < 9; col += 1) {
        const x = side * (15 + row * 6.4 + (random() - 0.5) * 0.4);
        const z = 16 - col * 7.0 + (random() - 0.5) * 0.6;
        const stack = 1 + Math.floor(random() * random() * 3.4);
        const ry = (random() - 0.5) * 0.05;
        for (let level = 0; level < stack; level += 1) {
          placements.push({
            x,
            y: 1.28 + level * 2.56,
            z,
            ry,
            color: CONTAINER_COLORS[Math.floor(random() * CONTAINER_COLORS.length)] ?? RUST,
          });
        }
      }
    }
  }
  const mesh = new THREE.InstancedMesh(geometry, material, placements.length);
  const dummy = new THREE.Object3D();
  placements.forEach((placement, index) => {
    dummy.position.set(placement.x, placement.y, placement.z);
    dummy.rotation.set(0, placement.ry, 0);
    dummy.updateMatrix();
    mesh.setMatrixAt(index, dummy.matrix);
    mesh.setColorAt(index, new THREE.Color(placement.color));
  });
  if (mesh.instanceColor) mesh.instanceColor.needsUpdate = true;
  return mesh;
}

// ---------------------------------------------------------------------------
// Gantry cranes: dark steel silhouettes straddling the quay edge, each with
// amber work lights and a blinking red aircraft beacon on the boom tip.
// ---------------------------------------------------------------------------

export function buildCranes(THREE: ThreeModule, glowTexture: Texture) {
  const group = new THREE.Group();
  const steel = new THREE.MeshStandardMaterial({
    color: 0x1a2026,
    roughness: 0.42,
    metalness: 0.72,
    envMapIntensity: 0.8,
  });
  const struts: Array<{ a: Vector3; b: Vector3; r: number }> = [];
  const beacons: Array<{ sprite: Sprite; light: InstanceType<ThreeModule["PointLight"]>; phase: number }> = [];
  const v = (x: number, y: number, z: number) => new THREE.Vector3(x, y, z);

  type StrutList = Array<{ a: Vector3; b: Vector3; r: number }>;

  const makeTower = (into: StrutList, cx: number, cz: number, half: number, height: number) => {
    for (const sx of [-1, 1]) {
      for (const sz of [-1, 1]) {
        into.push({ a: v(cx + sx * half, 0, cz + sz * half), b: v(cx + sx * half, height, cz + sz * half), r: 0.28 });
      }
    }
    const levels = Math.max(2, Math.round(height / 3.6));
    for (let i = 1; i <= levels; i += 1) {
      const y = (height / levels) * i;
      for (const sz of [-1, 1]) {
        into.push({ a: v(cx - half, y, cz + sz * half), b: v(cx + half, y, cz + sz * half), r: 0.12 });
      }
      for (const sx of [-1, 1]) {
        into.push({ a: v(cx + sx * half, y, cz - half), b: v(cx + sx * half, y, cz + half), r: 0.12 });
      }
      // Alternating diagonals on the two faces the camera sees.
      const y0 = (height / levels) * (i - 1);
      const flip = i % 2 === 0 ? 1 : -1;
      into.push({ a: v(cx - flip * half, y0, cz - half), b: v(cx + flip * half, y, cz - half), r: 0.09 });
      into.push({ a: v(cx - flip * half, y0, cz + half), b: v(cx + flip * half, y, cz + half), r: 0.09 });
    }
  };

  const makeTruss = (into: StrutList, points: Vector3[], depth: Vector3, r: number) => {
    // Two parallel chords plus a zig-zag web between them.
    for (let i = 1; i < points.length; i += 1) {
      const a = points[i - 1];
      const b = points[i];
      if (!a || !b) continue;
      const a2 = a.clone().add(depth);
      const b2 = b.clone().add(depth);
      into.push({ a, b, r });
      into.push({ a: a2, b: b2, r });
      into.push({ a, b: b2, r: r * 0.5 });
      into.push({ a: a2, b, r: r * 0.5 });
    }
    for (const point of points) {
      into.push({ a: point, b: point.clone().add(depth), r: r * 0.5 });
    }
  };

  const makeCrane = (x: number, z: number, height: number, span: number) => {
    const local: StrutList = [];
    const crane = new THREE.Group();
    const halfSpan = span / 2;
    for (const lx of [-halfSpan, halfSpan]) {
      for (const lz of [-3.4, 3.4]) makeTower(local, lx, lz, 1.05, height);
    }
    // Cross girder spanning the towers, then the boom out over the water.
    const girder: Vector3[] = [];
    for (let i = 0; i <= 8; i += 1) girder.push(v(-halfSpan - 1 + (i * (span + 2)) / 8, height + 1.6, 0));
    makeTruss(local, girder, v(0, -2.9, 0), 0.2);

    const boomStart = v(0, height + 0.6, -4);
    const boomEnd = v(0, height + 3.4, -28);
    const boomPoints: Vector3[] = [boomStart];
    for (let i = 1; i <= 4; i += 1) {
      const t = i / 4;
      boomPoints.push(
        boomStart
          .clone()
          .lerp(boomEnd, t)
          .setX(-0.9 + (1.8 * i) / 4),
      );
    }
    makeTruss(local, boomPoints, v(0, -1.7, 0), 0.16);

    // A-frame mast and tie cables: the unmistakable crane silhouette.
    const apex = v(0, height + 10, 1.6);
    for (const sx of [-1.4, 1.4]) {
      local.push({ a: v(sx, height + 1.6, 1.6), b: apex, r: 0.16 });
    }
    local.push({ a: apex, b: boomEnd, r: 0.06 });
    local.push({ a: apex, b: v(0, height + 1.6, 13), r: 0.06 });

    const cab = new THREE.Mesh(
      new THREE.BoxGeometry(1.5, 1.1, 1.5),
      new THREE.MeshStandardMaterial({ color: SIGNAL_HOT, emissive: SIGNAL, emissiveIntensity: 2.4 }),
    );
    cab.position.set(0, height - 2.4, -3.4);
    crane.add(cab);
    for (const lx of [-halfSpan / 2, 0, halfSpan / 2]) {
      const dot = new THREE.Mesh(
        new THREE.BoxGeometry(0.45, 0.45, 0.45),
        new THREE.MeshStandardMaterial({ color: SIGNAL_HOT, emissive: SIGNAL, emissiveIntensity: 1.6 }),
      );
      dot.position.set(lx, height - 0.2, 0);
      crane.add(dot);
    }
    const glow = new THREE.Sprite(
      new THREE.SpriteMaterial({
        map: glowTexture,
        color: SIGNAL,
        blending: THREE.AdditiveBlending,
        transparent: true,
        depthWrite: false,
        opacity: 0.5,
      }),
    );
    glow.position.set(0, height - 2.4, -3.4);
    glow.scale.set(8, 8, 1);
    crane.add(glow);

    const beacon = new THREE.Mesh(
      new THREE.SphereGeometry(0.3, 8, 8),
      new THREE.MeshStandardMaterial({ color: ALARM, emissive: ALARM, emissiveIntensity: 2 }),
    );
    beacon.position.copy(boomEnd);
    crane.add(beacon);
    const beaconGlow = new THREE.Sprite(
      new THREE.SpriteMaterial({
        map: glowTexture,
        color: ALARM,
        blending: THREE.AdditiveBlending,
        transparent: true,
        depthWrite: false,
        opacity: 0.8,
      }),
    );
    beaconGlow.position.copy(boomEnd);
    beaconGlow.scale.set(4, 4, 1);
    crane.add(beaconGlow);
    const beaconLight = new THREE.PointLight(ALARM, 12, 24, 2);
    beaconLight.position.copy(boomEnd);
    crane.add(beaconLight);
    beacons.push({ sprite: beaconGlow, light: beaconLight, phase: randomPhase() });

    // Lift the crane-local members into world space, then place the crane.
    const offset = v(x, 0, z);
    for (const strut of local) {
      struts.push({ a: strut.a.add(offset), b: strut.b.add(offset), r: strut.r });
    }
    crane.position.set(x, 0, z);
    group.add(crane);
  };

  makeCrane(-24, 4, 27, 17);
  makeCrane(30, 2, 31, 19);
  makeCrane(-62, 6, 25, 15);

  // All lattice members share one unit cylinder and one instanced draw call.
  const unit = new THREE.CylinderGeometry(1, 1, 1, 6);
  const lattice = new THREE.InstancedMesh(unit, steel, struts.length);
  const dummy = new THREE.Object3D();
  const up = new THREE.Vector3(0, 1, 0);
  const dir = new THREE.Vector3();
  struts.forEach((strut, index) => {
    dir.subVectors(strut.b, strut.a);
    const length = dir.length();
    dummy.position.copy(strut.a).addScaledVector(dir, 0.5);
    dummy.quaternion.setFromUnitVectors(up, dir.normalize());
    dummy.scale.set(strut.r, length, strut.r);
    dummy.updateMatrix();
    lattice.setMatrixAt(index, dummy.matrix);
  });
  lattice.frustumCulled = false;
  group.add(lattice);

  return {
    group,
    update(elapsed: number) {
      for (const beacon of beacons) {
        const on = Math.sin(elapsed * 1.4 + beacon.phase) > 0.55 ? 1 : 0.06;
        beacon.sprite.material.opacity = 0.8 * on;
        beacon.light.intensity = 12 * on;
      }
    },
  };
}

function randomPhase(): number {
  return Math.random() * Math.PI * 2;
}

// ---------------------------------------------------------------------------
// Ship at berth: a long dark hull with a lit superstructure, moored beyond
// the cranes so the fog takes most of its detail.
// ---------------------------------------------------------------------------

export function buildShip(THREE: ThreeModule, glowTexture: Texture): Group {
  const group = new THREE.Group();
  const hullMat = new THREE.MeshStandardMaterial({ color: 0x14181d, roughness: 0.55, metalness: 0.35 });
  const hull = new THREE.Mesh(new THREE.BoxGeometry(9, 5.5, 64), hullMat);
  hull.position.y = 1.9;
  group.add(hull);
  const bow = new THREE.Mesh(new THREE.ConeGeometry(4.4, 10, 4), hullMat);
  bow.rotation.x = -Math.PI / 2;
  bow.rotation.y = Math.PI / 4;
  bow.position.set(0, 1.9, -36);
  group.add(bow);
  const castle = new THREE.Mesh(new THREE.BoxGeometry(7, 6.5, 8), hullMat);
  castle.position.set(0, 7.8, 24);
  group.add(castle);
  const bridge = new THREE.Mesh(
    new THREE.BoxGeometry(6.4, 1.1, 6.5),
    new THREE.MeshStandardMaterial({ color: 0x39424c, roughness: 0.4, emissive: 0xffc07a, emissiveIntensity: 0.5 }),
  );
  bridge.position.set(0, 11.4, 24);
  group.add(bridge);

  // A sparse row of warm portholes along the hull.
  const portMat = new THREE.MeshStandardMaterial({ color: SIGNAL_HOT, emissive: SIGNAL, emissiveIntensity: 1.3 });
  for (let i = 0; i < 12; i += 1) {
    const dot = new THREE.Mesh(new THREE.BoxGeometry(0.16, 0.5, 0.7), portMat);
    dot.position.set(i % 2 === 0 ? 4.56 : -4.56, 2.6, -26 + i * 4.4);
    group.add(dot);
  }
  const mastLight = new THREE.Sprite(
    new THREE.SpriteMaterial({
      map: glowTexture,
      color: SIGNAL_HOT,
      blending: THREE.AdditiveBlending,
      transparent: true,
      depthWrite: false,
      opacity: 0.75,
    }),
  );
  mastLight.position.set(0, 13.4, 24);
  mastLight.scale.set(4.5, 4.5, 1);
  group.add(mastLight);

  group.position.set(14, -0.4, -74);
  group.rotation.y = 0.06;
  return group;
}

// ---------------------------------------------------------------------------
// Lamp masts along the central lane: sodium-amber heads, soft glow sprites,
// volumetric beam cones tilted with the dawn, and long wet reflections.
// ---------------------------------------------------------------------------

export function buildLampMasts(
  THREE: ThreeModule,
  glowTexture: Texture,
  streakTexture: Texture,
  beamTexture: Texture,
) {
  const group = new THREE.Group();
  const poleMat = new THREE.MeshStandardMaterial({ color: 0x1f242a, roughness: 0.5, metalness: 0.5 });
  const headMat = new THREE.MeshStandardMaterial({
    color: SIGNAL_HOT,
    emissive: SIGNAL,
    emissiveIntensity: 3.2,
    roughness: 0.3,
  });
  const pools: Sprite[] = [];

  const masts: Array<[number, number]> = [
    [-7.5, 6],
    [7.5, -6],
    [-7.5, -18],
    [7.5, -30],
  ];
  for (const [x, z] of masts) {
    const mast = new THREE.Group();
    const pole = new THREE.Mesh(new THREE.CylinderGeometry(0.14, 0.2, 11, 8), poleMat);
    pole.position.y = 5.5;
    mast.add(pole);
    const arm = new THREE.Mesh(new THREE.CylinderGeometry(0.09, 0.09, 2.4, 6), poleMat);
    arm.rotation.z = Math.PI / 2;
    arm.position.set(x < 0 ? 1.0 : -1.0, 10.8, 0);
    mast.add(arm);
    const head = new THREE.Mesh(new THREE.BoxGeometry(1.1, 0.28, 0.5), headMat);
    head.position.set(x < 0 ? 2.0 : -2.0, 10.65, 0);
    mast.add(head);

    const glow = new THREE.Sprite(
      new THREE.SpriteMaterial({
        map: glowTexture,
        color: SIGNAL,
        blending: THREE.AdditiveBlending,
        transparent: true,
        depthWrite: false,
        opacity: 0.85,
      }),
    );
    glow.position.copy(head.position);
    glow.scale.set(9, 9, 1);
    mast.add(glow);

    // Volumetric shaft: a camera-facing gradient that reads as light haze
    // falling from the head — a cone's silhouette edges would be hard.
    const shaft = new THREE.Sprite(
      new THREE.SpriteMaterial({
        map: beamTexture,
        color: SIGNAL,
        blending: THREE.AdditiveBlending,
        transparent: true,
        depthWrite: false,
        opacity: 0.26,
      }),
    );
    shaft.position.set(head.position.x * 0.9, 5.3, 0);
    shaft.scale.set(7, 11, 1);
    mast.add(shaft);

    // Wet-ground reflection: a long additive streak under the lamp.
    const pool = new THREE.Sprite(
      new THREE.SpriteMaterial({
        map: streakTexture,
        color: SIGNAL,
        blending: THREE.AdditiveBlending,
        transparent: true,
        depthWrite: false,
        opacity: 0.5,
      }),
    );
    pool.position.set(head.position.x, 0.06, 0.4);
    pool.scale.set(2.2, 7.5, 1);
    pools.push(pool);
    mast.add(pool);

    const light = new THREE.PointLight(SIGNAL, 30, 34, 2);
    light.position.set(head.position.x, 10.2, 0);
    mast.add(light);

    mast.position.set(x, 0, z);
    group.add(mast);
  }

  return {
    group,
    update(elapsed: number) {
      // Sodium flicker, very subtle, so the lights feel alive.
      const flicker = 1 + Math.sin(elapsed * 2.3) * 0.02 + Math.sin(elapsed * 7.1) * 0.012;
      for (const pool of pools) pool.material.opacity = 0.5 * flicker;
    },
  };
}

// ---------------------------------------------------------------------------
// Mist banks drift low over the water and between container rows.
// ---------------------------------------------------------------------------

export function buildMist(THREE: ThreeModule, mistTexture: Texture, random: () => number) {
  const group = new THREE.Group();
  const states: Array<{ sprite: Sprite; baseX: number; baseY: number; z: number; phase: number; amp: number }> = [];
  for (let i = 0; i < MIST_COUNT; i += 1) {
    const sprite = new THREE.Sprite(
      new THREE.SpriteMaterial({
        map: mistTexture,
        transparent: true,
        depthWrite: false,
        opacity: 0.08 + random() * 0.1,
        color: 0x8fa0b0,
      }),
    );
    const size = 40 + random() * 60;
    sprite.scale.set(size, size * 0.4, 1);
    const z = -14 - random() * 120;
    const baseX = (random() - 0.5) * 160;
    const baseY = 1.5 + random() * 7;
    sprite.position.set(baseX, baseY, z);
    group.add(sprite);
    states.push({ sprite, baseX, baseY, z, phase: random() * Math.PI * 2, amp: 5 + random() * 12 });
  }
  return {
    group,
    update(elapsed: number) {
      for (const state of states) {
        state.sprite.position.x = state.baseX + Math.sin(elapsed * 0.03 + state.phase) * state.amp;
        state.sprite.position.y = state.baseY + Math.sin(elapsed * 0.11 + state.phase) * 0.4;
      }
    },
  };
}

// ---------------------------------------------------------------------------
// Rain: thin instanced streaks slanting through the lamplight.
// ---------------------------------------------------------------------------

export function buildRain(THREE: ThreeModule) {
  const geometry = new THREE.PlaneGeometry(0.022, 1.4);
  const material = new THREE.MeshBasicMaterial({
    color: 0xc6d4e0,
    blending: THREE.AdditiveBlending,
    transparent: true,
    depthWrite: false,
    opacity: 0.3,
    side: THREE.DoubleSide,
  });
  const mesh = new THREE.InstancedMesh(geometry, material, RAIN_COUNT);
  mesh.instanceMatrix.setUsage(THREE.DynamicDrawUsage);
  const drops: Array<{ x: number; y: number; z: number; speed: number; slant: number }> = [];
  for (let i = 0; i < RAIN_COUNT; i += 1) {
    drops.push({
      x: (Math.random() - 0.5) * 70,
      y: Math.random() * 26,
      z: 30 - Math.random() * 70,
      speed: 16 + Math.random() * 10,
      slant: 0.18 + Math.random() * 0.1,
    });
  }
  const dummy = new THREE.Object3D();
  return {
    mesh,
    update(_elapsed: number, delta = 0.016) {
      for (let i = 0; i < drops.length; i += 1) {
        const drop = drops[i];
        if (!drop) continue;
        drop.y -= drop.speed * delta;
        drop.x += drop.speed * drop.slant * delta;
        if (drop.y < 0) {
          drop.y = 22 + Math.random() * 6;
          drop.x = (Math.random() - 0.5) * 70;
          drop.z = 30 - Math.random() * 70;
        }
        dummy.position.set(drop.x, drop.y, drop.z);
        dummy.rotation.set(0, 0, -drop.slant);
        dummy.updateMatrix();
        mesh.setMatrixAt(i, dummy.matrix);
      }
      mesh.instanceMatrix.needsUpdate = true;
    },
  };
}
