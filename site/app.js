// Draws the phone's screens for the site: the same stages, weather and HUD
// as the app (android/.../Scenes.kt, DashboardView.kt), in its 640×360 dp
// landscape coordinates. Original drawings only.
(() => {
  'use strict';
  const W = 640, H = 360;
  const C = { primary: '#E8E8E8', secondary: '#8C8C8C', teal: '#45B5A0', green: '#30D158', red: '#FF453A', amber: '#FFB340', blue: '#4DA3FF', purple: '#B06BFF' };
  const STATUS = [
    { k: 'green', label: 'PASSING', color: C.green },
    { k: 'amber', label: 'RUNNING', color: C.amber },
    { k: 'red', label: 'FAILED', color: C.red },
    { k: 'blue', label: 'WAITING', color: C.blue },
    { k: 'purple', label: 'NO LINK', color: C.purple },
  ];
  const STAGES = [
    { k: 'namek', name: 'Namek', note: 'three suns' },
    { k: 'wasteland', name: 'Wasteland', note: 'saiyan plains' },
    { k: 'ring', name: 'Tournament', note: 'the ring' },
    { k: 'lookout', name: 'Lookout', note: 'above the clouds' },
    { k: 'top', name: 'Tournament of Power', note: 'the void' },
  ];
  const reduced = matchMedia('(prefers-reduced-motion: reduce)').matches;
  const hour = new Date().getHours();

  // ---- helpers ------------------------------------------------------------------
  const rng = seed => () => { seed |= 0; seed = seed + 0x6D2B79F5 | 0; let t = Math.imul(seed ^ seed >>> 15, 1 | seed); t = t + Math.imul(t ^ t >>> 7, 61 | t) ^ t; return ((t ^ t >>> 14) >>> 0) / 4294967296; };
  const rgba = (hex, a) => { const n = parseInt(hex.slice(1), 16); return `rgba(${n >> 16},${n >> 8 & 255},${n & 255},${a})`; };
  const mix = (a, b, t) => { const p = x => parseInt(x.slice(1), 16), A = p(a), B = p(b); const ch = s => Math.round(((A >> s) & 255) * (1 - t) + ((B >> s) & 255) * t); return '#' + ((1 << 24) | ch(16) << 16 | ch(8) << 8 | ch(0)).toString(16).slice(1); };
  const statusOf = k => STATUS.find(s => s.k === k);
  function sky(ctx, top, horizon, y) { const g = ctx.createLinearGradient(0, 0, 0, y); g.addColorStop(0, top); g.addColorStop(1, horizon); ctx.fillStyle = g; ctx.fillRect(0, 0, W, y + 1); }
  function glow(ctx, x, y, r, color) { const g = ctx.createRadialGradient(x, y, 0, x, y, r); g.addColorStop(0, color); g.addColorStop(1, 'rgba(0,0,0,0)'); ctx.fillStyle = g; ctx.beginPath(); ctx.arc(x, y, r, 0, 7); ctx.fill(); }
  function disc(ctx, x, y, r, color) { ctx.fillStyle = color; ctx.beginPath(); ctx.arc(x, y, r, 0, 7); ctx.fill(); }
  function shape(ctx, pts, fill, rim, rimW = 1.2) { ctx.beginPath(); pts.forEach(([x, y], i) => i ? ctx.lineTo(x, y) : ctx.moveTo(x, y)); ctx.closePath(); ctx.fillStyle = fill; ctx.fill(); if (rim) { ctx.strokeStyle = rim; ctx.lineWidth = rimW; ctx.stroke(); } }
  function stars(ctx, seed, n, maxA) { const r = rng(seed); for (let i = 0; i < n; i++) disc(ctx, r() * W, r() * H * 0.7, r() < .15 ? 1.1 : .6, `rgba(221,238,255,${(.1 + r() * .9) * maxA})`); }
  function tint(h) {
    const keys = [[0, '#000000'], [5, '#000000'], [6, '#1a0d1c'], [7, '#20120a'], [9, '#07121e'], [16, '#07121e'], [18, '#22100a'], [19, '#1a0a14'], [21, '#000000'], [24, '#000000']];
    for (let i = 0; i < keys.length - 1; i++) { const [h0, c0] = keys[i], [h1, c1] = keys[i + 1]; if (h >= h0 && h < h1) return mix(c0, c1, (h - h0) / (h1 - h0)); }
    return '#000000';
  }
  const night = h => h >= 21 || h < 6;

  // ---- stages --------------------------------------------------------------------
  function tree(ctx, x, y, h, fill, rim) {
    ctx.strokeStyle = fill; ctx.lineWidth = 1.6; ctx.beginPath(); ctx.moveTo(x, y); ctx.lineTo(x, y - h); ctx.stroke();
    for (const [dx, dy, r] of [[0, -h - 6, 7], [-6, -h - 1, 5], [6, -h - 2, 5]]) { ctx.beginPath(); ctx.arc(x + dx, y + dy, r, 0, 7); ctx.fillStyle = fill; ctx.fill(); ctx.strokeStyle = rim; ctx.lineWidth = .8; ctx.stroke(); }
  }
  function pillar(ctx, x, base, w, h, fill, rim) { const top = base - h; shape(ctx, [[x - w * .35, base], [x - w * .28, top + 14], [x - w * .55, top + 4], [x - w * .5, top], [x + w * .5, top], [x + w * .55, top + 4], [x + w * .28, top + 14], [x + w * .35, base]], fill, rim); }
  const SCENES = {
    namek: { ground: 250, draw(ctx, h) {
      sky(ctx, mix('#000000', tint(h), .6), mix('#0b2a1a', tint(h), .3), 250);
      for (const [x, y, r] of [[110, 78, 9], [292, 46, 6], [528, 96, 11]]) { glow(ctx, x, y, r * 5, 'rgba(190,255,200,0.07)'); disc(ctx, x, y, r, 'rgba(225,255,215,0.20)'); }
      const w = ctx.createLinearGradient(0, 250, 0, H); w.addColorStop(0, '#081a11'); w.addColorStop(1, '#000'); ctx.fillStyle = w; ctx.fillRect(0, 250, W, H - 250);
      const r = rng(3); ctx.strokeStyle = 'rgba(130,230,180,0.07)'; ctx.lineWidth = 1;
      for (let i = 0; i < 40; i++) { const x = r() * W, y = 256 + r() * 100, l = 8 + r() * 30; ctx.beginPath(); ctx.moveTo(x, y); ctx.lineTo(x + l, y); ctx.stroke(); }
      for (const [x, ht, wd] of [[70, 70, 34], [250, 50, 26], [400, 90, 40], [590, 60, 30]]) { pillar(ctx, x, 252, wd, ht, '#0a1f15', 'rgba(60,140,100,0.16)'); tree(ctx, x - 6, 252 - ht, 12, '#0a1f15', 'rgba(60,140,100,0.16)'); tree(ctx, x + 8, 252 - ht, 16, '#0a1f15', 'rgba(60,140,100,0.16)'); }
      for (const [x, ht, wd] of [[160, 150, 60], [505, 125, 54]]) { pillar(ctx, x, 262, wd, ht, '#04100a', 'rgba(70,170,120,0.22)'); tree(ctx, x - 12, 262 - ht, 20, '#04100a', 'rgba(70,170,120,0.22)'); tree(ctx, x + 10, 262 - ht, 28, '#04100a', 'rgba(70,170,120,0.22)'); }
    } },
    wasteland: { ground: 270, draw(ctx, h) {
      sky(ctx, mix('#000000', tint(h), .6), mix('#1c0f07', tint(h), .3), 235);
      glow(ctx, 470, 230, 90, 'rgba(255,140,60,0.08)'); disc(ctx, 470, 230, 26, 'rgba(255,150,80,0.10)');
      const layer = (seed, base, amp, fill, rim) => { const r = rng(seed), pts = [[0, H]]; let x = 0; while (x < W + 40) { const plateau = r() < .45; const nx = x + 30 + r() * 70; const ny = plateau ? base - amp * (.5 + r() * .5) : base - r() * amp * .3; pts.push([x + 10, ny], [nx - 10, ny]); x = nx; } pts.push([W, H]); shape(ctx, pts, fill, rim); };
      layer(11, 245, 60, '#120a05', 'rgba(160,90,40,0.10)'); layer(12, 275, 70, '#0b0603', 'rgba(170,100,50,0.15)');
      ctx.fillStyle = '#050302'; ctx.fillRect(0, 300, W, 60);
      const r = rng(13);
      for (let i = 0; i < 9; i++) { const x = r() * W, y = 304 + r() * 46, rx = 6 + r() * 22; ctx.beginPath(); ctx.ellipse(x, y, rx, rx * .45, 0, Math.PI, 0); ctx.fillStyle = '#0a0503'; ctx.fill(); ctx.strokeStyle = 'rgba(180,110,60,0.14)'; ctx.lineWidth = 1; ctx.stroke(); }
      ctx.strokeStyle = 'rgba(180,110,60,0.10)'; ctx.beginPath(); ctx.ellipse(160, 335, 70, 12, 0, 0, 7); ctx.stroke();
    } },
    ring: { ground: 250, draw(ctx, h) {
      sky(ctx, mix('#000000', tint(h), .6), mix('#0a1222', tint(h), .3), 215);
      if (night(h)) stars(ctx, 21, 40, .25);
      const roof = (cx, y, w, rh, fill, rim) => { ctx.beginPath(); ctx.moveTo(cx - w, y); ctx.quadraticCurveTo(cx - w * .55, y - rh * .3, cx - w * .3, y - rh); ctx.lineTo(cx + w * .3, y - rh); ctx.quadraticCurveTo(cx + w * .55, y - rh * .3, cx + w, y); ctx.closePath(); ctx.fillStyle = fill; ctx.fill(); ctx.strokeStyle = rim; ctx.lineWidth = 1.2; ctx.stroke(); };
      ctx.fillStyle = '#070b14'; ctx.fillRect(250, 168, 140, 50);
      roof(320, 172, 110, 26, '#070b14', 'rgba(70,100,160,0.20)'); roof(320, 140, 70, 22, '#070b14', 'rgba(70,100,160,0.20)');
      const palm = (x, base, ph, lean) => { ctx.strokeStyle = '#05080f'; ctx.lineWidth = 3; ctx.beginPath(); ctx.moveTo(x, base); ctx.quadraticCurveTo(x + lean * .5, base - ph * .6, x + lean, base - ph); ctx.stroke();
        for (let k = 0; k < 6; k++) { const a = Math.PI + k * Math.PI / 5; ctx.lineWidth = 2; ctx.strokeStyle = 'rgba(70,100,160,0.18)'; ctx.beginPath(); ctx.moveTo(x + lean, base - ph); ctx.quadraticCurveTo(x + lean + Math.cos(a) * 18, base - ph - 10, x + lean + Math.cos(a) * 30, base - ph + 8 + Math.abs(Math.sin(a)) * 6); ctx.stroke(); } };
      palm(70, 250, 90, 12); palm(110, 250, 70, -10); palm(560, 250, 95, -14);
      ctx.fillStyle = '#03050a'; ctx.fillRect(0, 248, W, H - 248);
      shape(ctx, [[175, 258], [465, 258], [590, 352], [50, 352]], '#0c0f16', 'rgba(150,170,210,0.20)', 1.4);
      ctx.strokeStyle = 'rgba(150,170,210,0.06)'; ctx.lineWidth = 1;
      for (let i = 1; i < 8; i++) { const t = i / 8; ctx.beginPath(); ctx.moveTo(175 + 290 * t, 258); ctx.lineTo(50 + 540 * t, 352); ctx.stroke(); }
      for (let i = 1; i < 5; i++) { const t = i / 5, y = 258 + 94 * t * t * .9 + 94 * t * .1, f = (y - 258) / 94; ctx.beginPath(); ctx.moveTo(175 - 125 * f, y); ctx.lineTo(465 + 125 * f, y); ctx.stroke(); }
    } },
    lookout: { ground: 270, draw(ctx, h) {
      sky(ctx, mix('#000000', tint(h), .6), mix('#081428', tint(h), .3), 270);
      if (night(h)) stars(ctx, 31, 50, .3);
      const r = rng(32);
      for (let i = 0; i < 26; i++) { const x = r() * W, y = 268 + r() * 80, rx = 40 + r() * 70; ctx.save(); ctx.translate(x, y); ctx.scale(1, .35); const g = ctx.createRadialGradient(0, 0, 0, 0, 0, rx); g.addColorStop(0, 'rgba(130,160,215,0.10)'); g.addColorStop(1, 'rgba(0,0,0,0)'); ctx.fillStyle = g; ctx.beginPath(); ctx.arc(0, 0, rx, 0, 7); ctx.fill(); ctx.restore(); }
      const cx = 430, y = 150, rw = 130;
      ctx.fillStyle = '#060a14'; ctx.strokeStyle = 'rgba(90,120,190,0.22)'; ctx.lineWidth = 1.2;
      ctx.beginPath(); ctx.ellipse(cx, y, rw, 12, 0, 0, Math.PI * 2); ctx.fill();
      ctx.beginPath(); ctx.moveTo(cx - rw, y); ctx.quadraticCurveTo(cx - rw * .6, y + 70, cx, y + 78); ctx.quadraticCurveTo(cx + rw * .6, y + 70, cx + rw, y); ctx.closePath(); ctx.fill(); ctx.stroke();
      ctx.beginPath(); ctx.moveTo(cx - 6, y + 76); ctx.lineTo(cx, y + 118); ctx.lineTo(cx + 6, y + 76); ctx.fill(); ctx.stroke();
      ctx.beginPath(); ctx.arc(cx, y - 6, 34, Math.PI, 0); ctx.fill(); ctx.stroke();
      for (const dx of [-62, 62]) { ctx.beginPath(); ctx.arc(cx + dx, y - 4, 12, Math.PI, 0); ctx.fill(); ctx.stroke(); }
      ctx.beginPath(); ctx.moveTo(cx, y - 40); ctx.lineTo(cx, y - 54); ctx.stroke();
      ctx.lineWidth = 2;
      for (const px of [cx - 105, cx + 100]) { ctx.beginPath(); ctx.moveTo(px, y - 2); ctx.lineTo(px + 3, y - 34); ctx.stroke(); for (let k = 0; k < 5; k++) { ctx.beginPath(); ctx.moveTo(px + 3, y - 34); ctx.lineTo(px + 3 + (k - 2) * 9, y - 28 + Math.abs(k - 2) * 3); ctx.stroke(); } }
    } },
    top: { ground: 250, draw(ctx, h) {
      const g = ctx.createRadialGradient(320, 200, 10, 320, 200, 420); g.addColorStop(0, mix('#140b22', tint(h), .2)); g.addColorStop(1, '#000'); ctx.fillStyle = g; ctx.fillRect(0, 0, W, H);
      stars(ctx, 41, 90, .3);
      for (const [x, y, rr] of [[90, 70, 30], [560, 60, 18]]) { disc(ctx, x, y, rr, '#08050f'); ctx.strokeStyle = 'rgba(150,110,220,0.18)'; ctx.lineWidth = 1; ctx.beginPath(); ctx.arc(x, y, rr, 0, 7); ctx.stroke(); }
      ctx.beginPath(); ctx.ellipse(320, 262, 260, 52, 0, 0, 7);
      ctx.moveTo(440, 214); ctx.lineTo(500, 250); ctx.lineTo(470, 300); ctx.lineTo(410, 310); ctx.lineTo(430, 262); ctx.closePath();
      ctx.fillStyle = '#0a0712'; ctx.fill('evenodd'); ctx.strokeStyle = 'rgba(170,120,240,0.22)'; ctx.lineWidth = 1.3; ctx.stroke();
      ctx.fillStyle = '#050309'; ctx.beginPath(); ctx.ellipse(320, 276, 258, 52, 0, 0, Math.PI); ctx.fill();
      for (const [x, ph] of [[120, 40], [200, 52], [440, 50], [520, 38]]) { ctx.fillStyle = '#0a0712'; ctx.fillRect(x - 4, 262 - ph - 20, 8, ph); ctx.strokeStyle = 'rgba(170,120,240,0.18)'; ctx.strokeRect(x - 4, 262 - ph - 20, 8, ph); }
      const r = rng(42);
      for (let i = 0; i < 9; i++) { const x = r() * W, y = 120 + r() * 220, s = 6 + r() * 16, pts = []; for (let k = 0; k < 5; k++) { const a = k / 5 * Math.PI * 2 + r(); pts.push([x + Math.cos(a) * s * (.6 + r() * .6), y + Math.sin(a) * s * .6]); } shape(ctx, pts, '#0a0712', 'rgba(170,120,240,0.20)'); }
    } },
  };
  function starShape(ctx, x, y, r) { ctx.beginPath(); for (let k = 0; k < 10; k++) { const a = -Math.PI / 2 + k * Math.PI / 5, rr = k % 2 ? r * .45 : r; ctx.lineTo(x + Math.cos(a) * rr, y + Math.sin(a) * rr); } ctx.closePath(); ctx.fill(); }
  function wish(ctx) {
    const g = ctx.createLinearGradient(0, 0, 0, H); g.addColorStop(0, '#06040e'); g.addColorStop(1, '#000'); ctx.fillStyle = g; ctx.fillRect(0, 0, W, H);
    ctx.lineCap = 'round';
    const body = new Path2D(); body.moveTo(60, 340); body.bezierCurveTo(140, 180, 260, 330, 330, 200); body.bezierCurveTo(390, 90, 520, 180, 560, 90);
    ctx.strokeStyle = 'rgba(60,170,100,0.20)'; ctx.lineWidth = 30; ctx.stroke(body);
    ctx.strokeStyle = '#04110a'; ctx.lineWidth = 26; ctx.stroke(body);
    ctx.strokeStyle = 'rgba(60,170,100,0.10)'; ctx.lineWidth = 1; ctx.setLineDash([3, 6]); ctx.stroke(body); ctx.setLineDash([]); ctx.lineCap = 'butt';
    shape(ctx, [[548, 78], [600, 66], [612, 82], [588, 96], [560, 104]], '#04110a', 'rgba(60,170,100,0.24)');
    disc(ctx, 590, 78, 2.2, 'rgba(255,80,60,0.45)');
    ctx.strokeStyle = 'rgba(60,170,100,0.22)'; ctx.lineWidth = 1.2; ctx.beginPath(); ctx.moveTo(566, 72); ctx.quadraticCurveTo(560, 40, 540, 30); ctx.moveTo(578, 68); ctx.quadraticCurveTo(586, 36, 606, 26); ctx.stroke();
    [[120, 300], [190, 318], [262, 306], [330, 322], [400, 304], [468, 318], [536, 300]].forEach(([x, y], i) => {
      glow(ctx, x, y, 39, 'rgba(255,190,80,0.16)');
      const gg = ctx.createRadialGradient(x - 5, y - 5, 2, x, y, 15); gg.addColorStop(0, 'rgba(255,190,90,0.34)'); gg.addColorStop(1, 'rgba(150,70,0,0.26)'); ctx.fillStyle = gg; ctx.beginPath(); ctx.arc(x, y, 15, 0, 7); ctx.fill();
      ctx.fillStyle = 'rgba(255,60,40,0.42)';
      for (let s = 0; s <= i; s++) { const a = s / (i + 1) * Math.PI * 2, d = i ? 5 : 0; starShape(ctx, x + Math.cos(a) * d, y + Math.sin(a) * d, 2.4); }
    });
  }

  // ---- weather, aura ---------------------------------------------------------------
  function weather(ctx, status, ground) {
    const r = rng(77);
    if (status === 'amber') {
      for (let i = 0; i < 46; i++) { const x = r() * W, y = ground - r() * 200, l = 4 + r() * 12; ctx.strokeStyle = `rgba(255,179,64,${.05 + r() * .12})`; ctx.lineWidth = 1.2; ctx.beginPath(); ctx.moveTo(x, y); ctx.lineTo(x + (r() - .5) * 2, y - l); ctx.stroke(); }
    } else if (status === 'red') {
      const bolt = (x, y, len, w) => { ctx.beginPath(); ctx.moveTo(x, y); let bx = x, by = y; while (by < y + len) { bx += (r() - .5) * 26; by += 10 + r() * 16; ctx.lineTo(bx, by); } ctx.strokeStyle = 'rgba(255,120,110,0.07)'; ctx.lineWidth = w * 5; ctx.stroke(); ctx.strokeStyle = 'rgba(255,170,160,0.20)'; ctx.lineWidth = w; ctx.stroke(); return [bx, by]; };
      const [bx, by] = bolt(470, 0, 150, 1.4); bolt(bx, by - 40, 50, .8);
      ctx.beginPath(); let x = 0; ctx.moveTo(x, ground + 18); while (x < W) { x += 14 + r() * 24; ctx.lineTo(x, ground + 12 + r() * 16); }
      ctx.strokeStyle = 'rgba(255,69,58,0.07)'; ctx.lineWidth = 8; ctx.stroke(); ctx.strokeStyle = 'rgba(255,90,80,0.22)'; ctx.lineWidth = 1.2; ctx.stroke();
    } else if (status === 'purple') {
      for (let i = 0; i < 900; i++) { ctx.fillStyle = `rgba(176,107,255,${r() * .08})`; ctx.fillRect(r() * W, r() * H, 1.2, 1.2); }
      for (const yy of [80, 190, 300]) { ctx.fillStyle = 'rgba(176,107,255,0.025)'; ctx.fillRect(0, yy, W, 6); }
    } else if (status === 'blue') {
      const g = ctx.createLinearGradient(0, 0, 0, 120); g.addColorStop(0, 'rgba(77,163,255,0.08)'); g.addColorStop(1, 'rgba(77,163,255,0)'); ctx.fillStyle = g; ctx.fillRect(0, 0, W, 120);
    }
  }
  function aura(ctx, color) {
    const band = 46, strong = rgba(color, .19), clear = rgba(color, 0);
    const rect = (x0, y0, x1, y1, gx0, gy0, gx1, gy1) => { const g = ctx.createLinearGradient(gx0, gy0, gx1, gy1); g.addColorStop(0, strong); g.addColorStop(1, clear); ctx.fillStyle = g; ctx.fillRect(x0, y0, x1 - x0, y1 - y0); };
    rect(0, 0, W, band, 0, 0, 0, band); rect(0, H - band, W, H, 0, H, 0, H - band); rect(0, 0, band, H, 0, 0, band, 0); rect(W - band, 0, W, H, W, 0, W - band, 0);
  }
  // The phone paints scenes at a third of the resolution and lifts them to
  // 160 %; a soft blur and a brightness filter stand in for that here.
  function backdrop(ctx, stage, status, opts = {}) {
    ctx.fillStyle = '#000'; ctx.fillRect(0, 0, W, H);
    ctx.save();
    if ('filter' in ctx) ctx.filter = 'blur(0.8px) brightness(1.6)';
    if (opts.wish) wish(ctx); else SCENES[stage].draw(ctx, opts.hour ?? hour);
    ctx.restore();
    if (!opts.wish && opts.weather !== false) weather(ctx, status, SCENES[stage].ground);
    if (opts.aura !== false) aura(ctx, statusOf(status).color);
  }

  // ---- HUD -------------------------------------------------------------------------
  const MONO = '"Share Tech Mono", ui-monospace, monospace', SANS = 'Roboto, "IBM Plex Sans", system-ui, sans-serif';
  function brackets(ctx, x0, y0, x1, y1, len, color, w = 1.5) {
    ctx.strokeStyle = color; ctx.lineWidth = w; ctx.beginPath();
    ctx.moveTo(x0, y0 + len); ctx.lineTo(x0, y0); ctx.lineTo(x0 + len, y0);
    ctx.moveTo(x1 - len, y0); ctx.lineTo(x1, y0); ctx.lineTo(x1, y0 + len);
    ctx.moveTo(x1, y1 - len); ctx.lineTo(x1, y1); ctx.lineTo(x1 - len, y1);
    ctx.moveTo(x0 + len, y1); ctx.lineTo(x0, y1); ctx.lineTo(x0, y1 - len); ctx.stroke();
  }
  const NAMES = ['igris', 'scouter', 'recica.dev', 'sinjal', 'kurse', 'paleter', 'rritemi', 'startrail', 'bletaqa'];
  function projects(status) {
    const plan = { amber: [1, 4], red: [0, 5, 7] }[status] || [];
    return NAMES.map((n, i) => { const k = plan.includes(i) ? status : 'green'; return { name: n, k, color: statusOf(k).color }; });
  }
  function frame(ctx) { brackets(ctx, 10, 10, W - 10, H - 10, 22, 'rgba(69,181,160,0.55)', 1.2); }
  function focusHud(ctx, status, name = 'igris') {
    frame(ctx);
    const s = status === 'blue' || status === 'purple' ? statusOf('green') : statusOf(status);
    const ps = projects(status).filter(p => p.name !== name);
    ctx.font = `16px ${MONO}`; ctx.fillStyle = C.teal; ctx.fillText('SCAN ▸', 32, 48);
    ctx.font = `bold 30px ${SANS}`; ctx.fillStyle = C.primary; ctx.fillText(name, 98, 48);
    ctx.font = `30px ${MONO}`; ctx.fillText(status === 'red' ? '6750' : '9000', 532, 48);
    ctx.font = `16px ${MONO}`; ctx.fillStyle = C.teal; ctx.fillText('PWR', 492, 48);
    brackets(ctx, 32, 72, 389, 282, 18, s.color, 2);
    ctx.font = `bold 46px ${SANS}`; ctx.fillStyle = s.color; ctx.fillText(s.label, 54, 150);
    ctx.font = `18px ${MONO}`; ctx.fillStyle = C.secondary; ctx.fillText('CI · main · ' + (status === 'amber' ? '1m 12s' : '2m 14s'), 54, 188);
    ctx.font = `20px ${SANS}`; ctx.fillStyle = C.primary; ctx.fillText('fix: retry the feed on 502', 54, 222);
    ctx.font = `16px ${MONO}`; ctx.fillStyle = C.secondary; ctx.fillText('a3f9c21 · 14m ago · 2 PRs', 54, 254);
    ctx.font = `15px ${MONO}`; ctx.fillStyle = status === 'purple' ? C.purple : C.teal;
    ctx.fillText(status === 'purple' ? 'NO SIGNAL · 4m' : status === 'blue' ? '◆ 1 WAITING' : '◎ TRACKING', 418, 96);
    ps.slice(0, 4).forEach((o, i) => { const y = 134 + i * 38; disc(ctx, 422, y - 6, 4, o.color); ctx.font = `bold 18px ${SANS}`; ctx.fillStyle = C.primary; ctx.fillText(o.name, 434, y); ctx.font = `13px ${MONO}`; ctx.fillStyle = o.color; ctx.fillText(statusOf(o.k).label, 434, y + 16); });
  }
  function gridHud(ctx, status) {
    frame(ctx);
    const ps = projects(status);
    ctx.font = `16px ${MONO}`; ctx.fillStyle = C.teal; ctx.fillText(`SCAN ▸ ${ps.length + 3} TARGETS`, 28, 40);
    const all = status === 'green'; const right = all ? "IT'S OVER 9000!" : 'AVG PWR 7420';
    ctx.fillStyle = all ? C.green : C.teal; ctx.fillText(right, W - 28 - ctx.measureText(right).width, 40);
    const pad = 28, gap = 14, top = 56, tw = (W - pad * 2 - gap * 2) / 3, th = (H - top - 22 - gap * 2) / 3;
    ps.forEach((p, i) => { const l = pad + (i % 3) * (tw + gap), t = top + Math.floor(i / 3) * (th + gap);
      brackets(ctx, l, t, l + tw, t + th, 12, 'rgba(69,181,160,0.35)', 1.2); ctx.fillStyle = p.color; ctx.fillRect(l + 8, t + th * .2, 3, th * .6);
      ctx.font = `bold 21px ${SANS}`; ctx.fillStyle = C.primary; ctx.fillText(p.name, l + 22, t + th * .38);
      ctx.font = `bold 17px ${SANS}`; ctx.fillStyle = p.color; ctx.fillText(statusOf(p.k).label, l + 22, t + th * .66);
      ctx.font = `14px ${MONO}`; ctx.fillStyle = C.secondary; ctx.fillText((p.k === 'red' ? 'PWR 6750' : 'PWR 9000') + ' · 14m', l + 22, t + th * .9); });
    ctx.font = `15px ${MONO}`; ctx.fillStyle = C.teal; const more = '▼ 3 MORE'; ctx.fillText(more, (W - ctx.measureText(more).width) / 2, H - 6);
  }
  function agentsHud(ctx) {
    frame(ctx);
    ctx.font = `16px ${MONO}`; ctx.fillStyle = C.teal; ctx.fillText('AGENTS ▸ 3 SESSIONS', 28, 40);
    [['scouter', 'WAITING', C.blue, 'needs your answer · 2m'], ['igris', 'WORKING', C.amber, 'running tests · 6m'], ['recica.dev', 'DONE', C.green, 'finished · 9m ago']].forEach(([n, st, col, sub], i) => {
      const y = 98 + i * 82;
      if (i === 0) brackets(ctx, 20, y - 40, W - 20, y + 14, 12, col, 2);
      ctx.font = `bold 28px ${SANS}`; ctx.fillStyle = C.primary; ctx.fillText(n, 40, y);
      ctx.font = `bold 22px ${SANS}`; ctx.fillStyle = col; ctx.fillText(st, 430, y);
      ctx.font = `15px ${MONO}`; ctx.fillStyle = C.secondary; ctx.fillText(sub, 40, y + 24 - 4 + 2);
    });
  }
  function alertCard(ctx) {
    ctx.fillStyle = '#2a0503'; ctx.fillRect(0, 0, W, H);
    const g = ctx.createRadialGradient(W / 2, H / 2, 40, W / 2, H / 2, 420); g.addColorStop(0, 'rgba(255,69,58,0.25)'); g.addColorStop(1, 'rgba(0,0,0,0.6)'); ctx.fillStyle = g; ctx.fillRect(0, 0, W, H);
    // the crack
    ctx.strokeStyle = 'rgba(255,220,215,0.55)'; ctx.lineWidth = 1.4; ctx.beginPath();
    const r = rng(5); let x = 380, y = 0; ctx.moveTo(x, y); while (y < H) { x += (r() - .5) * 60; y += 20 + r() * 30; ctx.lineTo(x, y); } ctx.stroke();
    ctx.beginPath(); ctx.moveTo(420, 90); ctx.lineTo(500, 130); ctx.lineTo(560, 120); ctx.moveTo(360, 210); ctx.lineTo(300, 260); ctx.stroke();
    ctx.font = `16px ${MONO}`; ctx.fillStyle = '#FFC2C2'; ctx.fillText('⚠ POWER LEVEL DROPPING', 32, 52);
    ctx.font = `bold 50px ${SANS}`; ctx.fillStyle = '#fff'; ctx.fillText('recica.dev', 32, 124);
    ctx.font = `24px ${MONO}`; ctx.fillText('PWR 9000 → 6750', 32, 164);
    ctx.font = `24px ${SANS}`; ctx.fillStyle = '#FFE5E5'; ctx.fillText('Build failed on main:', 32, 208); ctx.fillText('bump next to 15.2', 32, 238);
    ctx.font = `15px ${MONO}`; ctx.fillStyle = '#FFC2C2'; ctx.fillText('TAP TO DISMISS', 32, 320);
  }

  // ---- hero: a live phone ---------------------------------------------------------------
  const hero = document.getElementById('hero');
  const state = { status: 'green', stage: 'namek', touched: false };
  const sweepFrom = { t: 1 };
  function paintHero(sweep = 1) {
    const ctx = hero.getContext('2d'); ctx.setTransform(hero.width / W, 0, 0, hero.height / H, 0, 0);
    backdrop(ctx, state.stage, state.status);
    focusHud(ctx, state.status);
    if (sweep < 1) { // the scan line that crosses the lens on every change
      const y = sweep * H; const g = ctx.createLinearGradient(0, y - 40, 0, y);
      g.addColorStop(0, 'rgba(143,240,220,0)'); g.addColorStop(1, 'rgba(143,240,220,0.18)');
      ctx.fillStyle = g; ctx.fillRect(0, y - 40, W, 40); ctx.fillStyle = 'rgba(143,240,220,0.5)'; ctx.fillRect(0, y, W, 1.2);
    }
    document.getElementById('led').style.setProperty('--c', statusOf(state.status).color);
  }
  function change() {
    syncChips();
    if (reduced) return paintHero();
    const t0 = performance.now();
    const step = now => { const t = Math.min(1, (now - t0) / 700); paintHero(t); if (t < 1) requestAnimationFrame(step); };
    requestAnimationFrame(step);
  }
  const sc = document.getElementById('status-chips');
  sc.innerHTML = STATUS.map(s => `<button class="chip" type="button" data-status="${s.k}" style="--c:${s.color}">${s.label}</button>`).join('');
  const stc = document.getElementById('stage-chips');
  stc.innerHTML = STAGES.map(s => `<button class="chip stage" type="button" data-stage="${s.k}">${s.name.toUpperCase()}</button>`).join('');
  function syncChips() {
    sc.querySelectorAll('[data-status]').forEach(b => b.setAttribute('aria-pressed', String(b.dataset.status === state.status)));
    stc.querySelectorAll('[data-stage]').forEach(b => b.setAttribute('aria-pressed', String(b.dataset.stage === state.stage)));
  }
  document.querySelector('.hero-phone').addEventListener('click', e => {
    const b = e.target.closest('[data-status],[data-stage]'); if (!b) return;
    state.touched = true;
    if (b.dataset.status) state.status = b.dataset.status;
    if (b.dataset.stage) state.stage = b.dataset.stage;
    change();
  });
  // Until someone touches it, the phone walks through a build: passing,
  // a push starts a run, it fails, the fix passes, on the next stage.
  const script = ['green', 'amber', 'red', 'amber', 'green'];
  let beat = 0;
  if (!reduced) setInterval(() => {
    if (state.touched || document.hidden) return;
    beat++;
    state.status = script[beat % script.length];
    if (beat % script.length === 0) state.stage = STAGES[(STAGES.findIndex(s => s.k === state.stage) + 1) % STAGES.length].k;
    change();
  }, 3800);

  // ---- static shots -----------------------------------------------------------------------
  function paintShots() {
    document.querySelectorAll('canvas.shot').forEach(cv => {
      const ctx = cv.getContext('2d'); ctx.setTransform(cv.width / W, 0, 0, cv.height / H, 0, 0);
      const kind = cv.dataset.shot;
      if (kind === 'focus') { backdrop(ctx, 'wasteland', 'amber', { hour: 18 }); focusHud(ctx, 'amber', 'scouter'); }
      if (kind === 'grid') { backdrop(ctx, 'lookout', 'red', { hour: 22 }); gridHud(ctx, 'red'); }
      if (kind === 'agents') { backdrop(ctx, 'top', 'blue', { hour: 22 }); agentsHud(ctx); }
      if (kind === 'alert') alertCard(ctx);
    });
    const gallery = document.getElementById('stage-gallery');
    const items = [...STAGES.map(s => ({ ...s })), { k: 'wish', name: 'The wish', note: 'all green' }];
    gallery.innerHTML = items.map(s => `<figure><canvas width="640" height="360" data-stage-shot="${s.k}" aria-label="${s.name}"></canvas><figcaption>${s.name}<span>${s.note.toUpperCase()}</span></figcaption></figure>`).join('');
    gallery.querySelectorAll('canvas').forEach((cv, i) => {
      const ctx = cv.getContext('2d'); ctx.setTransform(cv.width / W, 0, 0, cv.height / H, 0, 0);
      const k = cv.dataset.stageShot;
      const weatherFor = ['green', 'amber', 'red', 'green', 'purple'][i] || 'green';
      if (k === 'wish') backdrop(ctx, 'namek', 'green', { wish: true, aura: false });
      else backdrop(ctx, k, weatherFor, { hour: [21, 18, 12, 7, 22][i], aura: false });
    });
  }

  function all() { syncChips(); paintHero(); paintShots(); }
  all();
  if (document.fonts) document.fonts.ready.then(all);
})();
