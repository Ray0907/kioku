// Pure SVG builders. Input is numbers and roles only, so the strings are safe to inject.

/** @typedef {{ user: boolean, hit: boolean }} Tick */

const mix = (i) => 6 + ((i * 7919) % 7); // stable height for an unmatched tick

/** One session as a strip: you above, the agent below, matches lit. @param {Tick[]} ticks */
export function tape(ticks) {
  const n = ticks.length;
  if (!n) return '';
  const step = 400 / n;
  const w = Math.max(1.2, Math.min(7, step * 0.5));
  let out = `<svg class="tape" viewBox="0 0 400 30" width="100%" height="22" preserveAspectRatio="none" aria-hidden="true">`;
  ticks.forEach((t, i) => {
    const h = t.hit ? 12 : mix(i);
    const lane = t.user ? 1 : 17;
    const y = lane + (12 - h) / 2;
    const x = (i + 0.5) * step - w / 2;
    out += `<rect class="${t.hit ? 'h' : 't'}" x="${x.toFixed(2)}" y="${y.toFixed(1)}" width="${(w + (t.hit ? 1 : 0)).toFixed(2)}" height="${h}"/>`;
  });
  return out + '</svg>';
}

/** The same strip stood upright beside the transcript. */
export function minimap(ticks, cur, view) {
  const n = ticks.length;
  if (!n) return '';
  const H = 560;
  const step = H / n;
  const th = Math.max(2, step * 0.5);
  let out = `<svg class="mini" viewBox="0 0 40 ${H}" width="40" height="${H}" role="img" aria-label="Map of the session: ${n} messages">`;
  ticks.forEach((t, i) => {
    const hit = t.hit;
    const w = hit ? 16 : 8 + ((i * 7919) % 8);
    const x = (t.user ? 10 : 30) - w / 2;
    const cls = i === cur ? 'c' : hit ? 'h' : 't';
    out += `<rect class="${cls}" x="${x.toFixed(1)}" y="${(i * step + (step - th) / 2).toFixed(1)}" width="${w}" height="${th.toFixed(1)}"/>`;
  });
  if (view) {
    const y0 = view[0] * step;
    const y1 = (view[1] + 1) * step;
    out += `<rect class="view" x="0.5" y="${y0.toFixed(1)}" width="39" height="${Math.max(8, y1 - y0).toFixed(1)}" rx="4"/>`;
  }
  return out + '</svg>';
}

/** Hits per week. weeks: [{ start: 'YYYY-MM-DD', count }] oldest first. */
export function weekAxis(weeks, windowFrom, current, labels) {
  const W = 420;
  const H = 44;
  const n = weeks.length;
  const slot = (W - 8) / n;
  const max = Math.max(1, ...weeks.map((w) => w.count));
  const x = (i) => 4 + i * slot;
  let out = `<svg class="axis" viewBox="0 0 ${W} ${H + 18}" width="100%" role="img" aria-label="Hits per week over the last ${n} weeks">`;
  if (windowFrom != null) {
    out += `<rect class="win" x="${x(windowFrom).toFixed(1)}" y="1" width="${(W - 4 - x(windowFrom)).toFixed(1)}" height="${H - 2}" rx="6"/>`;
  }
  weeks.forEach((w, i) => {
    const h = w.count ? Math.max(3, (w.count / max) * (H - 8)) : 0;
    if (h) {
      const cls = ['wk', i === current ? 'on' : '', windowFrom != null && i < windowFrom ? 'out' : ''].join(' ').trim();
      out += `<rect class="${cls}" x="${(x(i) + 2).toFixed(1)}" y="${(H - 3 - h).toFixed(1)}" width="${Math.max(2, slot - 4).toFixed(1)}" height="${h.toFixed(1)}" rx="2"/>`;
    }
  });
  out += `<line class="base" x1="4" y1="${H}" x2="${W - 4}" y2="${H}"/>`;
  out += `<text class="lbl" x="4" y="${H + 14}">${labels[0]}</text><text class="lbl" x="${W - 4}" y="${H + 14}" text-anchor="end">${labels[1]}</text></svg>`;
  return out;
}
