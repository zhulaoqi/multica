/**
 * GLSL for the sign-in backdrop: procedural dawn sky, harbor water and the
 * grain + vignette composite pass. Kept in their own module so the scene
 * builder stays readable.
 */

export const SKY_VERTEX = /* glsl */ `
      varying vec3 vDir;
      void main() {
        vDir = position;
        vec4 p = projectionMatrix * mat4(mat3(viewMatrix)) * vec4(position, 1.0);
        gl_Position = p.xyww;
      }
    `;

export const SKY_FRAGMENT = /* glsl */ `
      varying vec3 vDir;
      uniform vec3 uSunDir;
      uniform float uTime;

      float hash(vec2 p) {
        p = fract(p * vec2(234.34, 435.345));
        p += dot(p, p + 34.23);
        return fract(p.x * p.y);
      }
      float vnoise(vec2 p) {
        vec2 i = floor(p);
        vec2 f = fract(p);
        vec2 u = f * f * (3.0 - 2.0 * f);
        float a = hash(i);
        float b = hash(i + vec2(1.0, 0.0));
        float c = hash(i + vec2(0.0, 1.0));
        float d = hash(i + vec2(1.0, 1.0));
        return mix(mix(a, b, u.x), mix(c, d, u.x), u.y);
      }
      float fbm(vec2 p) {
        float v = 0.0;
        float amp = 0.55;
        for (int i = 0; i < 4; i++) {
          v += amp * vnoise(p);
          p = p * 2.13 + vec2(17.3, 9.1);
          amp *= 0.5;
        }
        return v;
      }

      void main() {
        vec3 d = normalize(vDir);
        float sunAmt = max(dot(d, uSunDir), 0.0);
        float h = d.y;

        // Vertical gradient: ink above, steel middle, warm haze at the line.
        vec3 zenith = vec3(0.018, 0.024, 0.034);
        vec3 mid = vec3(0.085, 0.115, 0.16);
        vec3 horizonWarm = vec3(0.50, 0.30, 0.16);
        vec3 horizonCool = vec3(0.12, 0.155, 0.21);
        float warmSide = pow(sunAmt, 3.0);
        vec3 horizon = mix(horizonCool, horizonWarm, warmSide);
        float t1 = smoothstep(0.02, 0.34, h);
        float t0 = smoothstep(-0.25, 0.02, h);
        vec3 col = mix(horizon, mid, t1);
        col = mix(vec3(0.05, 0.06, 0.075), col, t0);
        col = mix(col, zenith, smoothstep(0.3, 0.85, h));

        // Cloud deck: a ridged band that opens toward the sun and burns on
        // the rim of the gap, like the reference's "burning hole" sky.
        if (h > -0.02) {
          vec2 cuv = d.xz / (h + 0.12) * vec2(0.55, 0.9) + vec2(uTime * 0.004, 0.0);
          float deck = fbm(cuv);
          // A second, finer octave gives the deck texture instead of a smear.
          float detail = fbm(cuv * 3.1 + vec2(4.2, 1.7));
          deck = deck * 0.72 + detail * 0.38;
          float band = smoothstep(0.015, 0.08, h) * (1.0 - smoothstep(0.34, 0.62, h));
          float cloud = smoothstep(0.46, 0.63, deck) * band;
          float gap = pow(sunAmt, 6.0);
          vec3 cloudDark = vec3(0.05, 0.06, 0.085);
          cloudDark += vec3(0.05, 0.07, 0.10) * (1.0 - cloud);
          vec3 rim = vec3(0.95, 0.52, 0.22) * (gap * 0.9 + pow(sunAmt, 3.0) * 0.35);
          col = mix(col, cloudDark, cloud * (1.0 - gap * 0.65));
          col += rim * cloud;
        }

        // Sun: hot core inside a broad aureole, kept small so the sign-in
        // card never sits on a blown-out field.
        float aureole = pow(sunAmt, 30.0) * 0.38 + pow(sunAmt, 300.0) * 1.1;
        col += vec3(1.0, 0.6, 0.28) * aureole;
        col += vec3(1.0, 0.85, 0.6) * smoothstep(0.9995, 0.9999, sunAmt) * 3.0;

        // Below the horizon: sea haze the water plane fades into.
        col = mix(col, horizon * 0.55, smoothstep(0.0, -0.2, h));

        gl_FragColor = vec4(col, 1.0);
      }
    `;

export const WATER_VERTEX = /* glsl */ `
      varying vec2 vUv;
      varying vec3 vWorld;
      void main() {
        vUv = uv;
        vec4 world = modelMatrix * vec4(position, 1.0);
        vWorld = world.xyz;
        gl_Position = projectionMatrix * viewMatrix * world;
      }
    `;

export const WATER_FRAGMENT = /* glsl */ `
      varying vec2 vUv;
      varying vec3 vWorld;
      uniform float uTime;
      uniform sampler2D uNoise;
      uniform vec3 uSunDir, uDeep, uSky, uWarm, uFog;
      void main() {
        vec2 uv = vUv * vec2(24.0, 16.0);
        float n1 = texture2D(uNoise, uv * 0.06 + vec2(uTime * 0.012, uTime * 0.004)).r;
        float n2 = texture2D(uNoise, uv * 0.17 - vec2(uTime * 0.008, uTime * 0.016)).r;
        float ripple = (n1 * 0.65 + n2 * 0.35);

        // Reflect a stylised sky: brighter toward the sun azimuth, a warm
        // smear under the sun, darker deep ink at steep view angles.
        vec3 viewDir = normalize(cameraPosition - vWorld);
        float fres = pow(1.0 - max(viewDir.y, 0.0), 2.0);
        float sunSide = pow(max(dot(normalize(vWorld - cameraPosition), normalize(vec3(uSunDir.x, 0.0, uSunDir.z))), 0.0), 3.0);
        vec3 col = mix(uDeep, uSky, fres * 0.8 + ripple * 0.12);
        col += uWarm * sunSide * (0.10 + ripple * 0.22) * (0.35 + fres);
        float sparkle = smoothstep(0.86, 0.99, ripple) * sunSide;
        col += uWarm * sparkle * 0.5;

        // Distance haze blends into the fog colour so the horizon melts.
        float dist = length(vWorld - cameraPosition);
        col = mix(col, uFog, smoothstep(90.0, 420.0, dist));
        gl_FragColor = vec4(col, 1.0);
      }
    `;

export const COMPOSITE_VERTEX = /* glsl */ `
      varying vec2 vUv;
      void main() {
        vUv = uv;
        gl_Position = vec4(position.xy, 0.0, 1.0);
      }
    `;

export const COMPOSITE_FRAGMENT = /* glsl */ `
      varying vec2 vUv;
      uniform sampler2D tScene;
      uniform sampler2D tBloom;
      uniform float uTime;
      uniform vec2 uRes;
      float hash(vec2 p) {
        p = fract(p * vec2(443.897, 441.423));
        p += dot(p, p.yx + 19.19);
        return fract((p.x + p.y) * p.x);
      }
      void main() {
        vec3 col = texture2D(tScene, vUv).rgb;
        col += texture2D(tBloom, vUv).rgb * 0.55;
        // Vignette: pulls the eye to the sun lane behind the sign-in card.
        vec2 q = vUv - 0.5;
        q.x *= uRes.x / max(uRes.y, 1.0);
        float vig = 1.0 - smoothstep(0.42, 1.05, length(q) * 1.35) * 0.45;
        col *= vig;
        // Animated film grain, a touch stronger in the shadows.
        float grain = hash(vUv * uRes * 0.35 + fract(uTime) * 61.7) - 0.5;
        col += grain * 0.035 * (1.0 - clamp(dot(col, vec3(0.333)), 0.0, 1.0) + 0.25);
        // Gentle warm lift so blacks never crush to pure zero.
        col = col * 0.985 + vec3(0.006, 0.004, 0.003);
        gl_FragColor = vec4(col, 1.0);
      }
    `;

/**
 * Bright-pass + cheap 9-tap blur for the composite's bloom. Rendered at a
 * quarter of the canvas size so the cost stays flat.
 */
export const BLOOM_FRAGMENT = /* glsl */ `
  varying vec2 vUv;
  uniform sampler2D tScene;
  uniform vec2 uTexel;
  uniform float uThreshold;

  vec3 bright(vec2 uv) {
    vec3 c = texture2D(tScene, uv).rgb;
    float luma = dot(c, vec3(0.299, 0.587, 0.114));
    return c * smoothstep(uThreshold, uThreshold + 0.35, luma);
  }

  void main() {
    vec3 sum = bright(vUv) * 0.28;
    sum += (bright(vUv + vec2(1.0, 0.6) * uTexel * 2.0) + bright(vUv - vec2(1.0, 0.6) * uTexel * 2.0)) * 0.14;
    sum += (bright(vUv + vec2(-1.0, 0.6) * uTexel * 2.0) + bright(vUv - vec2(-1.0, 0.6) * uTexel * 2.0)) * 0.14;
    sum += (bright(vUv + vec2(0.6, 1.0) * uTexel * 3.5) + bright(vUv - vec2(0.6, 1.0) * uTexel * 3.5)) * 0.09;
    sum += (bright(vUv + vec2(-0.6, 1.0) * uTexel * 3.5) + bright(vUv - vec2(-0.6, 1.0) * uTexel * 3.5)) * 0.09;
    gl_FragColor = vec4(sum, 1.0);
  }
`;
