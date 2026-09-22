<!--
  PetOverlay.vue — free-roam pixel critters that live over the whole UI.

  One pet per ACTIVE AGENT tab (flattened across every workspace from the
  terminalTabs store). Species is assigned deterministically by tab id, so the
  same agent always gets the same critter — a mixed zoo of cat / turtle / slime /
  ghost / duck / giraffe wandering along the bottom of the window.

  Behaviour is driven by the tab's TermStatus:
    running            → struts back and forth, dust kicks, "working…"
    waiting/permission → stops, bounces, "need input!" / "allow?"
    done/review        → hops with a sparkle + flag, "done!"
    error              → red shake, "oh no"
    idle               → slow shuffle, drifts to sleep (Zzz)

  Toggles (ui store):
    petsEnabled  — render the overlay at all (gated by parent)
    petsSpeech   — show the speech bubbles
    petsLeveling — pets grow + earn a crown the more turns their agent finishes

  Two sprite backends: inline-SVG species (the zoo) and image-sprite pets
  sliced from assets/*-pet-sprites.png (frames in src/assets/pets/). While
  TEST_ONLY_IMAGES is on, agents render only as image pets (cat / turtle),
  assigned by tab id. Overlay is pointer-events:none except the pets
  themselves (click to pet → wiggle).
-->
<template>
  <div class="pet-overlay" aria-hidden="true">
    <div
      v-for="p in pets"
      :key="p.id"
      class="pet"
      :class="`st-${p.status}`"
      :style="petStyle(p)"
      :title="`Go to ${tabTitle(p)}`"
      @click="poke(p)"
    >
      <div v-if="ui.petsSpeech && p.quip" class="pet-bubble">{{ p.quip }}</div>
      <div v-if="ui.petsLeveling && p.crown" class="pet-crown">♛</div>
      <div
        class="pet-sprite"
        :class="{ walk: p.moving, wiggle: p.wiggling }"
        :style="{ '--f': p.facing * (p.img ? 1 : FACE[p.species]) }"
      >
        <img v-if="p.img" :src="p.img" draggable="false" alt="" />
        <span v-else v-html="SPRITES[p.species]" />
      </div>
      <div class="pet-shadow" />
      <div v-if="p.status === 'idle'" class="pet-z">z</div>
      <div v-if="p.status === 'done' || p.status === 'review'" class="pet-spark">✦</div>
      <div v-if="ui.petsLeveling && p.level > 0" class="pet-lvl">Lv{{ p.level }}</div>
    </div>

    <!-- Farewell explosion when an agent tab closes: a critter goes out with a
         bang — radiating shards + a flash + smoke puff + a little "bye!". -->
    <div v-for="b in booms" :key="`b${b.id}-${b.born}`" class="boom" :style="boomStyle(b)">
      <div class="boom-flash" />
      <div class="boom-puff" />
      <i
        v-for="n in 8"
        :key="n"
        class="boom-shard"
        :style="{ '--a': `${(n - 1) * 45}deg`, '--c': b.color }"
      />
      <div class="boom-bye">bye!</div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, nextTick } from "vue";
import { useUIStore } from "@/stores/ui";
import { useTerminalTabsStore } from "@/stores/terminalTabs";
import { useWorkspaceStore } from "@/stores/workspace";
import type { TermStatus } from "@/lib/terminalStatus";

const ui = useUIStore();
const tabs = useTerminalTabsStore();
const wsStore = useWorkspaceStore();

// ── SVG sprite system ─────────────────────────────────────────────────────────
// Hand-drawn kawaii critters, one inline SVG per species (viewBox 0 0 48 48).
// Crisp at any scale — no pixel grid. Rendered at SPRITE px below.
const SPRITE_W = 64;
const SPRITE_H = 64;

interface Species {
  name: string;
  svg: string;
}

const SPECIES: Species[] = [
  {
    name: "cat",
    svg: `
      <polygon points="9,4 5,21 22,15" fill="#f6a23c"/>
      <polygon points="39,4 43,21 26,15" fill="#f6a23c"/>
      <polygon points="11,9 9,18 19,15" fill="#ff9bb3"/>
      <polygon points="37,9 39,18 29,15" fill="#ff9bb3"/>
      <circle cx="24" cy="29" r="16" fill="#f6a23c"/>
      <ellipse cx="24" cy="40" rx="14" ry="6" fill="#e07a2f" opacity=".25"/>
      <circle cx="16" cy="34" r="3" fill="#ff8fb0" opacity=".75"/>
      <circle cx="32" cy="34" r="3" fill="#ff8fb0" opacity=".75"/>
      <ellipse cx="18" cy="28" rx="3.3" ry="4.4" fill="#241c2b"/>
      <ellipse cx="30" cy="28" rx="3.3" ry="4.4" fill="#241c2b"/>
      <circle cx="19.3" cy="26.4" r="1.2" fill="#fff"/>
      <circle cx="31.3" cy="26.4" r="1.2" fill="#fff"/>
      <polygon points="24,31 22.3,33 25.7,33" fill="#ff6f93"/>
      <path d="M21.5 33.5 Q24 36 26.5 33.5" stroke="#241c2b" stroke-width="1.3" fill="none" stroke-linecap="round"/>
      <g stroke="#e07a2f" stroke-width="1" stroke-linecap="round">
        <line x1="5" y1="29" x2="13" y2="30"/><line x1="5" y1="33" x2="13" y2="33"/>
        <line x1="43" y1="29" x2="35" y2="30"/><line x1="43" y1="33" x2="35" y2="33"/>
      </g>`,
  },
  {
    name: "turtle",
    svg: `
      <ellipse cx="11" cy="40" rx="4" ry="3" fill="#7ec850"/>
      <ellipse cx="37" cy="40" rx="4" ry="3" fill="#7ec850"/>
      <ellipse cx="24" cy="40" rx="13" ry="4.5" fill="#3f7a2e" opacity=".25"/>
      <path d="M9 36 Q9 18 24 18 Q39 18 39 36 Z" fill="#5aa83a"/>
      <path d="M9 36 Q9 18 24 18 Q39 18 39 36 Z" fill="none" stroke="#3f7a2e" stroke-width="1.4"/>
      <g stroke="#3f7a2e" stroke-width="1.2" fill="none" stroke-linecap="round">
        <path d="M24 18 L24 36"/><path d="M14 33 Q19 23 24 24"/><path d="M34 33 Q29 23 24 24"/>
      </g>
      <polygon points="18,29 24,26 30,29 27,34 21,34" fill="#7ec850" opacity=".5"/>
      <ellipse cx="24" cy="40" rx="8" ry="5.5" fill="#9bdc6a"/>
      <ellipse cx="18" cy="38" rx="3.1" ry="3.6" fill="#9bdc6a"/>
      <circle cx="18" cy="37.6" r="1.7" fill="#241c2b"/>
      <circle cx="18.6" cy="36.9" r=".6" fill="#fff"/>
      <ellipse cx="30" cy="38" rx="3.1" ry="3.6" fill="#9bdc6a"/>
      <circle cx="30" cy="37.6" r="1.7" fill="#241c2b"/>
      <circle cx="30.6" cy="36.9" r=".6" fill="#fff"/>
      <path d="M22 41.5 Q24 43 26 41.5" stroke="#3f7a2e" stroke-width="1.1" fill="none" stroke-linecap="round"/>
      <circle cx="15" cy="41" r="1.6" fill="#ffa3b8" opacity=".6"/>
      <circle cx="33" cy="41" r="1.6" fill="#ffa3b8" opacity=".6"/>`,
  },
  {
    name: "slime",
    svg: `
      <path d="M7 38 Q5 15 24 13 Q43 15 41 38 Q41 43 36 43 L12 43 Q7 43 7 38 Z" fill="#5be08a"/>
      <path d="M9 40 Q24 46 39 40 Q39 42 36 43 L12 43 Q9 42 9 40 Z" fill="#2f9c5a" opacity=".55"/>
      <ellipse cx="18" cy="23" rx="4" ry="5.2" fill="#15402a"/>
      <ellipse cx="30" cy="23" rx="4" ry="5.2" fill="#15402a"/>
      <circle cx="19.6" cy="20.8" r="1.5" fill="#fff"/>
      <circle cx="31.6" cy="20.8" r="1.5" fill="#fff"/>
      <circle cx="13" cy="30" r="3" fill="#ff9bb3" opacity=".55"/>
      <circle cx="35" cy="30" r="3" fill="#ff9bb3" opacity=".55"/>
      <path d="M21.5 30 Q24 32.5 26.5 30" stroke="#15402a" stroke-width="1.3" fill="none" stroke-linecap="round"/>
      <ellipse cx="18" cy="19" rx="6" ry="3.4" fill="#fff" opacity=".4"/>`,
  },
  {
    name: "ghost",
    svg: `
      <path d="M9 25 Q9 7 24 7 Q39 7 39 25 L39 43 L33.5 38 L28 43 L24 38.5 L20 43 L14.5 38 L9 43 Z" fill="#edf0ff"/>
      <path d="M9 25 Q9 7 24 7 Q39 7 39 25 L39 32 Q24 36 9 32 Z" fill="#fff" opacity=".5"/>
      <ellipse cx="18" cy="23" rx="3.4" ry="4.6" fill="#3f37a8"/>
      <ellipse cx="30" cy="23" rx="3.4" ry="4.6" fill="#3f37a8"/>
      <circle cx="19.1" cy="21.3" r="1.2" fill="#fff"/>
      <circle cx="31.1" cy="21.3" r="1.2" fill="#fff"/>
      <circle cx="13" cy="29" r="2.6" fill="#ffb3c9" opacity=".75"/>
      <circle cx="35" cy="29" r="2.6" fill="#ffb3c9" opacity=".75"/>
      <ellipse cx="24" cy="30" rx="2.3" ry="2.9" fill="#6b63c9"/>`,
  },
  {
    name: "duck",
    svg: `
      <g stroke="#fb923c" stroke-width="1.7" stroke-linecap="round" fill="none">
        <path d="M19 41 l-3 5 M19 41 l0 5 M19 41 l3 5"/>
        <path d="M30 41 l-3 5 M30 41 l0 5 M30 41 l3 5"/>
      </g>
      <ellipse cx="25" cy="30" rx="15" ry="13" fill="#ffd23f"/>
      <ellipse cx="25" cy="40" rx="12" ry="4.5" fill="#f0b400" opacity=".35"/>
      <path d="M33 27 Q42 28 39 37 Q33 37 31 31 Z" fill="#f0b400"/>
      <circle cx="22" cy="17" r="10.5" fill="#ffd23f"/>
      <path d="M13 15 Q5 16.5 5 19 Q5 21.5 13 21 Z" fill="#fb923c"/>
      <path d="M5.5 18.6 Q9 19 13 18.6" stroke="#e2761f" stroke-width=".9" fill="none"/>
      <circle cx="21" cy="15" r="2.7" fill="#241c2b"/>
      <circle cx="22" cy="14.1" r=".95" fill="#fff"/>
      <circle cx="27" cy="20" r="2.6" fill="#ffb37a" opacity=".7"/>`,
  },
  {
    name: "giraffe",
    svg: `
      <g stroke="#c9892f" stroke-width="2.6" stroke-linecap="round">
        <line x1="20" y1="40" x2="20" y2="46"/><line x1="29" y1="40" x2="29" y2="46"/>
      </g>
      <rect x="16" y="30" width="17" height="12" rx="5" fill="#f4c560"/>
      <ellipse cx="24.5" cy="41.5" rx="9" ry="3" fill="#c9892f" opacity=".22"/>
      <path d="M22 32 Q19 20 26 10" fill="none" stroke="#f4c560" stroke-width="7" stroke-linecap="round"/>
      <g fill="#b5701f">
        <circle cx="20" cy="33" r="2.1"/><circle cx="28" cy="35" r="2.3"/>
        <circle cx="22" cy="26" r="1.8"/><circle cx="24" cy="20" r="1.6"/>
        <circle cx="29" cy="38" r="1.6"/>
      </g>
      <path d="M24 14 Q21 9 25 6 Q30 8 30 13 Z" fill="#f4c560"/>
      <ellipse cx="30" cy="11" rx="4.5" ry="4" fill="#f7d488"/>
      <path d="M33 11 Q37 11 37 13 Q35 14 32 13 Z" fill="#dba35a"/>
      <g stroke="#c9892f" stroke-width="1.6" stroke-linecap="round">
        <line x1="26" y1="7" x2="25" y2="3"/><line x1="30" y1="6.5" x2="30" y2="2.5"/>
      </g>
      <circle cx="25" cy="3" r="1.6" fill="#7c5e3c"/><circle cx="30" cy="2.5" r="1.6" fill="#7c5e3c"/>
      <ellipse cx="22" cy="6" rx="2.4" ry="3" fill="#f4c560"/>
      <circle cx="29" cy="10" r="1.7" fill="#241c2b"/>
      <circle cx="29.6" cy="9.4" r=".6" fill="#fff"/>
      <circle cx="33" cy="13" r="1.4" fill="#ffb37a" opacity=".7"/>`,
  },
];

const SPRITES = SPECIES.map(
  (s) => `<svg viewBox="0 0 48 48" xmlns="http://www.w3.org/2000/svg">${s.svg}</svg>`,
);

// ── Image-sprite pets (sliced from assets/*-pet-sprites.png) ──────────────────
// Each pet is a frame set, one entry per behaviour. While TEST_ONLY_IMAGES is
// on, agents render only as these image pets (no SVG zoo), assigned by tab id.
import catWork0 from "@/assets/pets/cat-work-0.png";
import catWork1 from "@/assets/pets/cat-work-1.png";
import catIdle from "@/assets/pets/cat-idle.png";
import catAttn from "@/assets/pets/cat-attn.png";
import catHappy from "@/assets/pets/cat-happy.png";
import turtleWork0 from "@/assets/pets/turtle-work-0.png";
import turtleWork1 from "@/assets/pets/turtle-work-1.png";
import turtleIdle from "@/assets/pets/turtle-idle.png";
import turtleAttn from "@/assets/pets/turtle-attn.png";
import turtleHappy from "@/assets/pets/turtle-happy.png";

interface ImagePet {
  run: string[]; // running — multi-frame loop
  idle: string[];
  attn: string[]; // waiting / permission / error
  happy: string[]; // done / review
}
const IMAGE_PETS: ImagePet[] = [
  { run: [catWork0, catWork1], idle: [catIdle], attn: [catAttn], happy: [catHappy] },
  { run: [turtleWork0, turtleWork1], idle: [turtleIdle], attn: [turtleAttn], happy: [turtleHappy] },
];

const TEST_ONLY_IMAGES = false;

// Pick the image-pet frame for a status; `now` cycles the running loop.
function imageFrame(petIdx: number, status: TermStatus, now: number): string {
  const p = IMAGE_PETS[petIdx];
  if (status === "running") return p.run[Math.floor(now / 14) % p.run.length];
  if (status === "done" || status === "review") return p.happy[0];
  if (status === "waiting" || status === "permission" || status === "error") return p.attn[0];
  return p.idle[0];
}

// Which way each sprite is drawn (1 = right). Front-facing critters are 1 (a
// flip is invisible); the duck is in profile facing LEFT, so it's -1 — its
// render flip = facing * FACE keeps the beak pointing where it walks.
const FACE = [1, 1, 1, 1, -1, 1]; // cat, turtle, slime, ghost, duck, giraffe

// Shard tint per species (matches each sprite's body) for the farewell boom.
const BOOM_COLOR = ["#f6a23c", "#5aa83a", "#5be08a", "#edf0ff", "#ffd23f", "#f4c560"];

// ── Runtime state per pet (preserved across re-renders, keyed by tab id) ───────
interface PetRT {
  x: number; // px from left
  vx: number; // px/frame
  facing: 1 | -1;
  level: number;
  finished: number; // completed turns counted toward leveling
  lastStatus: TermStatus;
  wiggleUntil: number;
}
const rt = new Map<number, PetRT>();
let frame = 0;

interface PetView {
  id: number;
  wsId: number;
  species: number;
  status: TermStatus;
  x: number;
  facing: 1 | -1;
  moving: boolean;
  wiggling: boolean;
  quip: string;
  level: number;
  crown: boolean;
  img?: string; // image-sprite frame (cat); undefined → SVG species
}

const pets = ref<PetView[]>([]);

// ── Farewell explosions ───────────────────────────────────────────────────────
// When a pet's tab closes the critter "explodes". A boom lives ~0.7s then clears.
interface Boom { id: number; born: number; x: number; color: string }
const booms = ref<Boom[]>([]);
const BOOM_LIFE = 42; // frames (~0.7s @ 60fps)

const QUIP: Partial<Record<TermStatus, string>> = {
  running: "working…",
  waiting: "need input!",
  permission: "allow?",
  done: "done!",
  review: "look!",
  error: "oh no",
};

// Flatten every agent tab across all workspaces → the live pet roster.
function roster(): { id: number; wsId: number; status: TermStatus }[] {
  const out: { id: number; wsId: number; status: TermStatus }[] = [];
  for (const [ws, list] of Object.entries(tabs.tabsByWs)) {
    for (const t of list) if (t.isAgent) out.push({ id: t.id, wsId: Number(ws), status: t.status });
  }
  return out;
}

function viewport() {
  return window.innerWidth || 1280;
}

function step() {
  frame++;
  const now = frame;
  const w = viewport();
  const live = roster();
  const liveIds = new Set(live.map((r) => r.id));
  // Drop runtime for pets whose tab vanished — and send them off with a bang.
  for (const id of [...rt.keys()]) {
    if (!liveIds.has(id)) {
      const r = rt.get(id)!;
      if (ui.petsEnabled) {
        const species = ((id % SPECIES.length) + SPECIES.length) % SPECIES.length;
        booms.value.push({ id, born: now, x: r.x, color: BOOM_COLOR[species] });
      }
      rt.delete(id);
    }
  }
  // Expire spent explosions.
  if (booms.value.length) booms.value = booms.value.filter((b) => now - b.born < BOOM_LIFE);

  const views: PetView[] = [];
  for (const { id, wsId, status } of live) {
    let r = rt.get(id);
    if (!r) {
      r = {
        x: Math.abs((id * 137) % Math.max(1, w - SPRITE_W)),
        vx: 0.9,
        facing: 1,
        level: 0,
        finished: 0,
        lastStatus: status,
        wiggleUntil: 0,
      };
      rt.set(id, r);
    }

    // Leveling: count each fresh transition into a finished state.
    if (status !== r.lastStatus) {
      if ((status === "done" || status === "review") && ui.petsLeveling) {
        r.finished++;
        if (r.finished % 2 === 0) r.level++;
      }
      r.lastStatus = status;
    }

    // Motion model:
    //   running → brisk strut across the window
    //   idle    → ambles slowly ~1/3 of the time, then pauses
    // Everything else holds position with its own CSS animation.
    let moving = false;
    let speed = 0;
    if (status === "running") { moving = true; speed = r.vx; }
    else if (status === "idle" && ((now + id * 53) % 320) < 120) { moving = true; speed = 0.32; }

    if (moving) {
      r.x += speed * r.facing;
      const max = Math.max(1, w - SPRITE_W);
      if (r.x <= 0) { r.x = 0; r.facing = 1; }
      else if (r.x >= max) { r.x = max; r.facing = -1; }
      // occasional direction flip for life
      if ((id * now) % 521 === 0) r.facing = (r.facing === 1 ? -1 : 1);
    }

    views.push({
      id,
      wsId,
      species: ((id % SPECIES.length) + SPECIES.length) % SPECIES.length,
      status,
      x: r.x,
      facing: r.facing,
      moving,
      wiggling: now < r.wiggleUntil,
      quip: QUIP[status] ?? "",
      level: r.level,
      crown: ui.petsLeveling && r.level >= 3,
      img: TEST_ONLY_IMAGES
        ? imageFrame(((id % IMAGE_PETS.length) + IMAGE_PETS.length) % IMAGE_PETS.length, status, now)
        : undefined,
    });
  }
  pets.value = views;
  raf = requestAnimationFrame(step);
}

// Click a pet → wiggle + jump to its agent's terminal (switch workspace, then
// activate the tab once its Terminal is mounted — same path as the Sidebar).
function poke(p: PetView) {
  const r = rt.get(p.id);
  if (r) r.wiggleUntil = frame + 30;
  const ws = wsStore.workspaces.find((w) => w.id === p.wsId);
  if (!ws) return;
  if (ui.mode !== "terminal") ui.setMode("terminal");
  if (wsStore.active?.id !== ws.id) wsStore.open(ws);
  nextTick(() => tabs.activate(ws.id, p.id));
}

function petStyle(p: PetView) {
  return { left: `${p.x}px`, width: `${SPRITE_W}px`, height: `${SPRITE_H}px` };
}

function boomStyle(b: Boom) {
  // Centre the burst on where the critter's body was.
  return { left: `${b.x + SPRITE_W / 2}px`, bottom: `${4 + SPRITE_H / 2}px` };
}

function tabTitle(p: PetView): string {
  const list = tabs.tabsByWs[p.wsId] || [];
  return list.find((t) => t.id === p.id)?.title || "terminal";
}


let raf = 0;
onMounted(() => { raf = requestAnimationFrame(step); });
onBeforeUnmount(() => cancelAnimationFrame(raf));
</script>

<style scoped>
.pet-overlay {
  position: fixed;
  inset: 0;
  pointer-events: none;
  z-index: 5; /* above panels, below modals/spotlight/toasts */
  overflow: hidden;
}

.pet {
  position: absolute;
  bottom: 4px;
  /* width/height set inline from SPRITE_W/H */
  pointer-events: auto;
  cursor: pointer;
  filter: drop-shadow(0 2px 2px rgba(0, 0, 0, 0.4));
  transition: transform 0.15s ease;
}

.pet-sprite {
  position: absolute;
  inset: 0;
}
.pet-sprite :deep(svg) {
  width: 100%;
  height: 100%;
  display: block;
}
.pet-sprite img,
.pet-sprite span {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  display: block;
}
.pet-sprite img {
  object-fit: contain;
  object-position: 50% 100%; /* feet on the floor line */
  user-select: none;
}

/* ── Facing + walk live on the SPRITE, so the .pet state-bounces never mirror
      the art and the speech bubble stays upright. --f = +1/-1 from inline style. */
.pet-sprite { transform: scaleX(var(--f, 1)); transform-origin: 50% 100%; }

.pet-sprite.walk { animation: pet-waddle 0.34s ease-in-out infinite; }
@keyframes pet-waddle {
  0%, 100% { transform: scaleX(var(--f, 1)) translateY(0)    rotate(-6deg); }
  25%      { transform: scaleX(var(--f, 1)) translateY(-3px) rotate(0deg); }
  50%      { transform: scaleX(var(--f, 1)) translateY(0)    rotate(6deg); }
  75%      { transform: scaleX(var(--f, 1)) translateY(-3px) rotate(0deg); }
}

/* click-to-poke wiggle (one-shot; overrides walk while it plays) */
.pet-sprite.wiggle { animation: pet-wiggle 0.45s ease; }
@keyframes pet-wiggle {
  0%, 100% { transform: scaleX(var(--f, 1)) rotate(0); }
  20%      { transform: scaleX(var(--f, 1)) rotate(-18deg); }
  55%      { transform: scaleX(var(--f, 1)) rotate(14deg); }
  80%      { transform: scaleX(var(--f, 1)) rotate(-7deg); }
}

/* ── State animations: vertical motion on the whole pet ──────────────────── */
/* waiting / permission: impatient bounce */
.pet.st-waiting, .pet.st-permission { animation: pet-jump 0.6s ease-in-out infinite; }
@keyframes pet-jump {
  0%, 100% { transform: translateY(0); }
  40%      { transform: translateY(-9px); }
}
.pet.st-permission { filter: drop-shadow(0 0 6px #fbbf24); }

/* done / review: celebratory hop + squash-stretch */
.pet.st-done, .pet.st-review { animation: pet-hop 0.5s ease-in-out infinite; }
@keyframes pet-hop {
  0%, 100% { transform: translateY(0) scale(1, 1); }
  20%      { transform: translateY(0) scale(1.12, 0.9); }
  50%      { transform: translateY(-10px) scale(0.94, 1.1); }
  80%      { transform: translateY(0) scale(1.08, 0.94); }
}

/* error: angry shake + red wash */
.pet.st-error { animation: pet-shake 0.1s linear infinite; }
.pet.st-error .pet-sprite { filter: hue-rotate(-55deg) saturate(2.2) brightness(0.95); }
@keyframes pet-shake {
  0%, 100% { transform: translateX(0) rotate(0); }
  25%      { transform: translateX(-2px) rotate(-3deg); }
  75%      { transform: translateX(2px)  rotate(3deg); }
}

/* idle: gentle breathing (only while standing still — strolling uses .walk) */
.pet.st-idle { opacity: 0.92; }
.pet.st-idle .pet-sprite:not(.walk) { animation: pet-breathe 2.6s ease-in-out infinite; }
@keyframes pet-breathe {
  0%, 100% { transform: scaleX(var(--f, 1)) scaleY(1)    translateY(0); }
  50%      { transform: scaleX(var(--f, 1)) scaleY(0.96) translateY(0.6px); }
}

/* ── Ground shadow (squashes as the pet leaves the floor) ────────────────── */
.pet-shadow {
  position: absolute;
  left: 50%;
  bottom: -2px;
  width: 64%;
  height: 6px;
  transform: translateX(-50%);
  background: radial-gradient(ellipse at center, rgba(0, 0, 0, 0.45), transparent 70%);
  border-radius: 50%;
  pointer-events: none;
  z-index: -1;
}
.pet.st-waiting .pet-shadow,
.pet.st-permission .pet-shadow { animation: shadow-jump 0.6s ease-in-out infinite; }
.pet.st-done .pet-shadow,
.pet.st-review .pet-shadow { animation: shadow-jump 0.5s ease-in-out infinite; }
@keyframes shadow-jump {
  0%, 100% { transform: translateX(-50%) scale(1);    opacity: 0.5; }
  45%      { transform: translateX(-50%) scale(0.55); opacity: 0.22; }
}

/* ── Decorations ───────────────────────────────────────────────────────────── */
.pet-bubble {
  position: absolute;
  bottom: calc(100% + 4px);
  left: 50%;
  transform: translateX(-50%);
  white-space: nowrap;
  background: var(--bg-panel, #1a1a1a);
  color: var(--text-base, #fff);
  border: 1px solid var(--border, #444);
  border-radius: 6px;
  padding: 1px 6px;
  font-size: 9px;
  font-weight: 600;
  line-height: 1.4;
  pointer-events: none;
}

.pet-crown {
  position: absolute;
  top: -14px;
  left: 50%;
  transform: translateX(-50%);
  font-size: 13px;
  color: #fbbf24;
  text-shadow: 0 1px 1px rgba(0,0,0,0.6);
  pointer-events: none;
}

.pet-lvl {
  position: absolute;
  bottom: -10px;
  left: 50%;
  transform: translateX(-50%);
  font-size: 7px;
  color: var(--text-dim, #aaa);
  pointer-events: none;
}

.pet-z {
  position: absolute;
  top: -6px;
  right: -4px;
  font-size: 10px;
  color: var(--text-dim, #99a);
  animation: pet-zfloat 2s ease-in-out infinite;
  pointer-events: none;
}
@keyframes pet-zfloat {
  0%   { opacity: 0; transform: translateY(2px); }
  50%  { opacity: 1; transform: translateY(-4px); }
  100% { opacity: 0; transform: translateY(-8px); }
}

.pet-spark {
  position: absolute;
  top: -8px;
  left: -2px;
  font-size: 10px;
  color: #fde047;
  animation: pet-zfloat 1s ease-in-out infinite;
  pointer-events: none;
}

/* ── Farewell explosion (tab closed) ─────────────────────────────────────────
   Anchored at the critter's old body centre; everything radiates from 0,0. */
.boom {
  position: absolute;
  width: 0;
  height: 0;
  pointer-events: none;
  z-index: 1;
}

/* central flash — bright pop that fades fast */
.boom-flash {
  position: absolute;
  left: 0;
  top: 0;
  width: 26px;
  height: 26px;
  margin: -13px 0 0 -13px;
  border-radius: 50%;
  background: radial-gradient(circle, #fff 0%, #fde047 45%, transparent 72%);
  animation: boom-flash 0.4s ease-out forwards;
}
@keyframes boom-flash {
  0%   { transform: scale(0.2); opacity: 1; }
  60%  { transform: scale(1.6); opacity: 0.9; }
  100% { transform: scale(2.4); opacity: 0; }
}

/* lingering smoke puff that swells and dissolves */
.boom-puff {
  position: absolute;
  left: 0;
  top: 0;
  width: 34px;
  height: 34px;
  margin: -17px 0 0 -17px;
  border-radius: 50%;
  background: radial-gradient(circle, rgba(190, 190, 200, 0.55), transparent 70%);
  animation: boom-puff 0.7s ease-out forwards;
}
@keyframes boom-puff {
  0%   { transform: scale(0.3) translateY(0); opacity: 0; }
  30%  { opacity: 0.7; }
  100% { transform: scale(1.7) translateY(-10px); opacity: 0; }
}

/* 8 shards flung outward along --a, tinted to the critter --c */
.boom-shard {
  position: absolute;
  left: 0;
  top: 0;
  width: 6px;
  height: 6px;
  margin: -3px 0 0 -3px;
  border-radius: 2px;
  background: var(--c, #fde047);
  transform: rotate(var(--a)) translateX(0);
  animation: boom-shard 0.6s cubic-bezier(0.2, 0.7, 0.3, 1) forwards;
}
@keyframes boom-shard {
  0%   { transform: rotate(var(--a)) translateX(2px) scale(1.2); opacity: 1; }
  100% { transform: rotate(var(--a)) translateX(30px) scale(0.2); opacity: 0; }
}

.boom-bye {
  position: absolute;
  left: 0;
  top: 0;
  transform: translate(-50%, -100%);
  white-space: nowrap;
  font-size: 9px;
  font-weight: 700;
  color: var(--text-base, #fff);
  text-shadow: 0 1px 2px rgba(0, 0, 0, 0.7);
  animation: boom-bye 0.7s ease-out forwards;
}
@keyframes boom-bye {
  0%   { opacity: 0; transform: translate(-50%, -60%) scale(0.6); }
  35%  { opacity: 1; transform: translate(-50%, -120%) scale(1); }
  100% { opacity: 0; transform: translate(-50%, -180%) scale(1); }
}
</style>
