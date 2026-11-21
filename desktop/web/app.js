// kioku desktop. The Go binary does the searching; this file draws what it returns.
import { tape, minimap, weekAxis } from './draw.js';
import { esc, CJK, fmtDate, tz, who, marked, queue } from './util.js';

/**
 * @typedef {{ ref: string, harness: string, project: string, age: string, hits: number,
 *   best: string, topic: string, highlights: {location:number,length:number}[] }} SessionRow
 * @typedef {{ time: string, role: string, text: string, matches: boolean,
 *   highlights?: {location:number,length:number}[] }} Message
 * @typedef {{ harness: string, project: string, topic: string, cwd: string, date: string,
 *   resume_cmd: string, messages: Message[], session_total: number }} Detail
 */

const invoke = window.__TAURI__.core.invoke;
const $ = (id) => document.getElementById(id);

const st = {
  query: '',
  /** @type {SessionRow[]} */ sessions: [],
  weeks: [],
  selected: '',
  cur: 0,
  gen: 0,
  /** @type {Map<string, Promise<Detail>>} */ details: new Map(),
  /** @type {Map<string, Detail>} */ loaded: new Map(),
};


const loadDetail = (ref) => {
  if (!st.details.has(ref)) {
    const q = st.query;
    const p = queue(() => invoke('show_session', { reference: ref, query: q })).then((d) => { if (q === st.query) st.loaded.set(ref, d); return d; });
    st.details.set(ref, p);
    p.catch(() => st.details.delete(ref));
  }
  return st.details.get(ref);
};

const ticksOf = (d) => d.messages.map((m) => ({ user: m.role === 'user', hit: !!m.matches }));
const matchIdx = (d) => d.messages.flatMap((m, i) => (m.matches ? [i] : []));

// ---------- search ----------

function terms(q) {
  return (q.match(/"[^"]*"|\S+/g) ?? []).map((t) => {
    const not = t.startsWith('-') && t.length > 1;
    const body = (not ? t.slice(1) : t).replace(/^"|"$/g, '');
    const chars = [...body];
    const title = CJK.test(body) && chars.length > 1
      ? 'Matched as the pairs ' + chars.slice(0, -1).map((c, i) => c + chars[i + 1]).join(', ')
      : '';
    return { body, not, title };
  });
}

function renderRead() {
  const ts = terms(st.query);
  const el = $('read');
  el.hidden = !ts.length;
  el.innerHTML = ts.length
    ? '<span>Read as</span>' + ts.map((t, i) =>
        (i ? '<span class="and">and</span>' : '') +
        `<span class="chip${t.not ? ' not' : ''}"${t.title ? ` title="${esc(t.title)}"` : ''}>${t.not ? 'not ' : ''}${esc(t.body)}</span>`).join('')
    : '';
}

function weeksFrom(facet) {
  const counts = new Map((facet ?? []).map((w) => [w.value, w.count]));
  const monday = new Date();
  monday.setHours(0, 0, 0, 0);
  monday.setDate(monday.getDate() - ((monday.getDay() + 6) % 7));
  const out = [];
  for (let i = 12; i >= 0; i--) {
    const d = new Date(monday);
    d.setDate(d.getDate() - 7 * i);
    const key = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
    out.push({ start: key, count: counts.get(key) ?? 0, date: d });
  }
  return out;
}

function renderAxis() {
  const el = $('axis');
  const weeks = weeksFrom(st.weeks);
  if (!weeks.some((w) => w.count)) { el.hidden = true; return; }
  const days = Number($('range').value) || 0;
  let from = null;
  if (days) {
    const cutoff = Date.now() - days * 86400000;
    from = Math.max(0, weeks.findIndex((w) => w.date.getTime() + 7 * 86400000 > cutoff));
  }
  const d = st.loaded.get(st.selected)?.date;
  const current = d ? weeks.findIndex((w, i) => d >= w.start && (i === weeks.length - 1 || d < weeks[i + 1].start)) : -1;
  const label = days ? `last ${days} days` : 'all time';
  el.hidden = false;
  el.innerHTML = `<div class="axishead"><b>Hits per week</b><small>Range: ${label}</small></div>` +
    weekAxis(weeks, from, current, [fmtDate(weeks[0].start), 'this week']);
}

function fillProjects(facet) {
  const sel = $('project');
  if (sel.value) return; // keep the list while a project filter is on
  sel.innerHTML = '<option value="">All projects</option>' +
    (facet ?? []).map((p) => `<option>${esc(p.value)}</option>`).join('');
}

function rowHtml(s) {
  const cap = s.hits ? `<span class="mono">${s.hits}</span> ${s.hits === 1 ? 'match' : 'matches'}` : '';
  return `<li><button type="button" class="row" data-ref="${esc(s.ref)}" aria-selected="${s.ref === st.selected}">` +
    `<span class="top"><span><span class="who">${esc(s.harness)}</span> in ${esc(s.project)}</span><span class="mono when">${esc(s.age)}</span></span>` +
    `<span class="tape-slot wait"></span><span class="cap">${cap}</span>` +
    `<span class="snip">${marked(s.best, s.highlights)}</span></button></li>`;
}

function fillRow(s, d) {
  const ref = s.ref;
  const row = document.querySelector(`.row[data-ref="${CSS.escape(ref)}"]`);
  if (!row) return;
  const slot = row.querySelector('.tape-slot');
  slot.classList.remove('wait');
  slot.innerHTML = tape(ticksOf(d));
  const hits = matchIdx(d);
  const first = d.messages[Number(s.best_ref.split(':')[1]) - (d.start ?? 0)]?.time ?? ''; // the snippet's message
  row.querySelector('.when').textContent = `${fmtDate(d.date)} ${first ? first + ' ' + tz : ''}`.trim();
  row.querySelector('.cap').innerHTML = hits.length
    ? `<span class="mono">${hits.length}</span> of <span class="mono">${d.messages.length}</span> messages match`
    : `<span class="mono">${d.messages.length}</span> messages`;
}

let observer;
function watchRows() {
  observer?.disconnect();
  observer = new IntersectionObserver((entries) => {
    for (const e of entries) {
      if (!e.isIntersecting) continue;
      const ref = e.target.dataset.ref;
      observer.unobserve(e.target);
      loadDetail(ref).then((d) => fillRow(st.sessions.find((x) => x.ref === ref), d)).catch(() => {});
    }
  }, { root: document.querySelector('.list') });
  document.querySelectorAll('.row').forEach((r) => observer.observe(r));
}

const box = (cls, h, p, btn) =>
  `<div class="msgbox ${cls}"${cls === 'err' ? ' role="alert"' : ''}><h2>${h}</h2><p>${esc(p)}</p>${btn ? `<button type="button" class="btn" id="retry">${btn}</button>` : ''}</div>`;

async function runSearch() {
  const gen = ++st.gen;
  st.details.clear(); st.loaded.clear(); // a detail carries this query's matches; never reuse it for the next one
  const results = $('results');
  results.dataset.state = 'loading';
  results.setAttribute('aria-busy', 'true');
  results.innerHTML = '<ul class="rows">' + '<li class="skel" aria-hidden="true"><i></i><i></i><i></i></li>'.repeat(5) + '</ul>';
  renderRead();
  let res;
  try {
    res = await invoke('search', {
      query: st.query,
      days: Number($('range').value) || null,
      project: $('project').value || null,
    });
  } catch (e) {
    if (gen !== st.gen) return;
    results.dataset.state = 'error';
    results.removeAttribute('aria-busy');
    results.innerHTML = box('err', 'Could not search', String(e), 'Try again');
    $('retry').onclick = runSearch;
    showSession(null);
    return;
  }
  if (gen !== st.gen) return; // a newer keystroke owns the screen
  results.removeAttribute('aria-busy');
  st.sessions = res.sessions.sessions ?? [];
  st.weeks = res.weeks?.week ?? [];
  fillProjects(res.sessions.facets?.project);
  if (!st.sessions.length) {
    results.dataset.state = 'empty';
    results.innerHTML = box('', st.query ? 'No message matches' : 'Nothing indexed yet',
      st.query ? 'Words are joined with AND. Remove one, or widen the range.' : 'Run kioku once in a terminal to build the index.',
      $('range').value ? 'Search all time' : '');
    if ($('retry')) $('retry').onclick = () => { $('range').value = ''; refresh(); };
    renderAxis();
    showSession(null);
    return;
  }
  results.dataset.state = 'full';
  if (!st.sessions.some((s) => s.ref === st.selected)) st.selected = st.sessions[0].ref;
  const total = res.sessions.total ?? st.sessions.length;
  // The message total is only exact when every session is on the page.
  const msgs = st.query && total === st.sessions.length ? st.sessions.reduce((n, s) => n + s.hits, 0) : 0;
  results.innerHTML = `<div class="count">${msgs ? `<span class="mono">${msgs}</span> messages in ` : ''}<span class="mono">${total}</span> sessions</div>` +
    '<ul class="rows">' + st.sessions.map(rowHtml).join('') + '</ul>';
  renderAxis();
  watchRows();
  // Draw the first screenful and the open session before returning, so tests can wait on it.
  await Promise.allSettled(st.sessions.slice(0, 6).map((s) => loadDetail(s.ref).then((d) => fillRow(s, d))));
  if (gen === st.gen) await showSession(st.selected);
}

// ---------- the open session ----------

let expanded = new Set();

async function showSession(ref) {
  const pane = $('session');
  $('none').hidden = !!ref;
  pane.hidden = !ref;
  if (!ref) return;
  st.selected = ref;
  document.querySelectorAll('.row').forEach((r) => r.setAttribute('aria-selected', String(r.dataset.ref === ref)));
  let d;
  try { d = await loadDetail(ref); } catch (e) {
    if (st.selected !== ref) return;
    pane.innerHTML = box('err', 'Could not open this session', String(e));
    return;
  }
  if (st.selected !== ref) return;
  expanded = new Set();
  st.cur = 0;
  renderAxis();
  renderSession(ref, d);
  goto(0, false);
}

function renderSession(ref, d) {
  const hits = matchIdx(d);
  const title = d.topic || '(untitled)';
  const cmd = (d.cwd ? `cd ${d.cwd} && ` : '') + d.resume_cmd;
  $('session').innerHTML =
    `<div class="reader-full">` +
    `<div class="rhead"><h1${CJK.test(title) ? ' lang="zh"' : ''}>${esc(title)}</h1>` +
    `<div class="acts">${hits.length ? `<div class="transport" role="group" aria-label="Matches in this session">` +
      `<button type="button" class="ico" id="prev" aria-label="Previous match"><svg width="14" height="14" viewBox="0 0 14 14" aria-hidden="true"><path d="M9 3 L5 7 L9 11" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/></svg></button>` +
      `<span class="mono" id="curn">1</span><span>of</span><span class="mono">${hits.length}</span>` +
      `<button type="button" class="ico" id="next" aria-label="Next match"><svg width="14" height="14" viewBox="0 0 14 14" aria-hidden="true"><path d="M5 3 L9 7 L5 11" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/></svg></button></div>` : ''}` +
    `<button type="button" class="btn btn--primary" id="resume"><svg width="10" height="12" viewBox="0 0 10 12" aria-hidden="true"><path d="M1 1 L9 6 L1 11 Z" fill="currentColor"/></svg>Resume<kbd>Enter</kbd></button></div>` +
    `<dl><div><dt>Agent</dt><dd>${esc(d.harness)}</dd></div><div><dt>Project</dt><dd class="mono">${esc(d.cwd || d.project)}</dd></div>` +
    `<div><dt>Started</dt><dd class="mono">${esc(fmtDate(d.date))}</dd></div><div><dt>Messages</dt><dd class="mono">${d.messages.length}</dd></div></dl>` +
    `<div class="cmd"><span class="mono">${esc(cmd)}</span><button type="button" class="btn copy" id="copy">Copy</button></div>` +
    `<small id="note" role="status"></small></div>` +
    `<div class="body"><div class="thread" id="thread"></div><div class="minimap" id="map"></div></div></div>`;
  $('resume').onclick = () => resume(ref);
  $('copy').onclick = async () => { await navigator.clipboard.writeText(cmd); $('note').textContent = 'Copied'; };
  if (hits.length) { $('prev').onclick = () => step(-1); $('next').onclick = () => step(1); }
  renderThread(d);
}

/** Matches and one neighbour each side stay visible; runs of other messages fold. */
function renderThread(d) {
  const n = d.messages.length;
  const hits = matchIdx(d);
  const show = new Set();
  if (hits.length) hits.forEach((i) => { for (const j of [i - 1, i, i + 1]) if (j >= 0 && j < n) show.add(j); });
  else for (let i = 0; i < Math.min(n, 30); i++) show.add(i);
  const name = who(d.harness);
  let html = '';
  let i = 0;
  while (i < n) {
    if (show.has(i) || expanded.has(i)) { html += msgHtml(d.messages[i], i, name); i++; continue; }
    let j = i;
    while (j < n && !show.has(j)) j++;
    if (expanded.has(i)) { i = j; continue; }
    const c = j - i;
    const noun = c === 1 ? 'message' : 'messages';
    const label = i === 0 ? `${c} earlier ${noun}` : j === n ? `${c} later ${noun}` : `${c} ${noun}`;
    html += `<button type="button" class="fold" data-from="${i}" data-to="${j}">${label}</button>`;
    i = j;
  }
  const thread = $('thread');
  thread.innerHTML = html;
  thread.querySelectorAll('.fold').forEach((b) => b.onclick = () => {
    for (let k = Number(b.dataset.from); k < Number(b.dataset.to); k++) { expanded.add(k); show.add(k); }
    renderThread(d);
    goto(st.cur, false);
  });
  thread.querySelectorAll('.more').forEach((b) => b.onclick = () => {
    const t = b.previousElementSibling; t.classList.toggle('clamp'); b.textContent = t.classList.contains('clamp') ? 'Show all' : 'Show less';
  });
  paintMap(d);
}

function msgHtml(m, i, name) {
  const cls = m.role === 'user' ? 'user' : m.role === 'tool' ? 'tool' : 'asst';
  const label = m.role === 'user' ? 'You' : m.role === 'tool' ? 'Tool' : name;
  const long = m.text.length > 900 || m.text.split('\n').length > 12;
  return `<div class="msg ${cls}${m.matches ? ' hit' : ''}" data-i="${i}"><div class="role">${label}<time>${esc(m.time)} ${tz}</time></div>` +
    `<div><pre class="text${long ? ' clamp' : ''}"${CJK.test(m.text) ? ' lang="zh"' : ''}>${marked(m.text, m.highlights)}</pre>${long ? '<button type="button" class="more">Show all</button>' : ''}</div></div>`;
}

function paintMap(d) {
  const hits = matchIdx(d);
  const cur = hits[st.cur] ?? -1;
  const vis = [...document.querySelectorAll('#thread .msg')].map((e) => Number(e.dataset.i));
  const view = vis.length ? [Math.min(...vis), Math.max(...vis)] : null;
  const map = $('map');
  map.innerHTML = minimap(ticksOf(d), cur, view);
  map.querySelector('svg')?.addEventListener('click', (e) => {
    const r = e.currentTarget.getBoundingClientRect();
    const idx = Math.floor(((e.clientY - r.top) / r.height) * d.messages.length);
    const k = hits.reduce((b, h, x) => (Math.abs(h - idx) < Math.abs(hits[b] - idx) ? x : b), 0);
    if (hits.length) goto(k, true);
  });
}

function step(dir) {
  const d = st.loaded.get(st.selected);
  if (!d) return;
  const n = matchIdx(d).length;
  if (n) goto((st.cur + dir + n) % n, true);
}

function goto(k, scroll) {
  const d = st.loaded.get(st.selected);
  if (!d) return;
  const hits = matchIdx(d);
  if (!hits.length) return;
  st.cur = k;
  $('curn').textContent = String(k + 1);
  document.querySelectorAll('#thread .msg.cur').forEach((e) => e.classList.remove('cur'));
  const el = document.querySelector(`#thread .msg[data-i="${hits[k]}"]`);
  el?.classList.add('cur');
  if (scroll) el?.scrollIntoView({ block: 'center', behavior: 'auto' });
  paintMap(d);
}

async function resume(ref) {
  try { await invoke('resume', { reference: ref }); $('note').textContent = 'Opened in Terminal'; }
  catch (e) { $('note').textContent = String(e); }
}

// ---------- wiring ----------

let timer;
const refresh = () => { st.query = $('q').value.trim(); return runSearch(); };
$('q').addEventListener('input', () => { clearTimeout(timer); timer = setTimeout(refresh, 180); });
$('range').addEventListener('change', refresh);
$('project').addEventListener('change', refresh);
$('results').addEventListener('click', (e) => {
  const row = e.target.closest('.row');
  if (row) showSession(row.dataset.ref);
});

document.addEventListener('keydown', (e) => {
  const typing = /^(INPUT|SELECT|TEXTAREA)$/.test(document.activeElement?.tagName ?? '');
  if (e.key === 'Escape' && typing) { document.activeElement.blur(); return; }
  if (typing || e.metaKey || e.ctrlKey || e.altKey) return;
  if (e.key === '/') { e.preventDefault(); $('q').focus(); $('q').select(); }
  else if (e.key === 'n') step(1);
  else if (e.key === 'N') step(-1);
  else if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
    e.preventDefault();
    const i = st.sessions.findIndex((s) => s.ref === st.selected);
    const next = st.sessions[Math.max(0, Math.min(st.sessions.length - 1, i + (e.key === 'ArrowDown' ? 1 : -1)))];
    if (next) { showSession(next.ref); document.querySelector(`.row[data-ref="${CSS.escape(next.ref)}"]`)?.scrollIntoView({ block: 'nearest' }); }
  } else if (e.key === 'Enter' && st.selected) resume(st.selected);
});

// ---------- end-to-end hook, inert unless the test script sets KIOKU_E2E_OUT ----------

function colors(sel) {
  const el = document.querySelector(sel);
  if (!el) return null;
  let bg = el;
  while (bg && /transparent|\/ [\d.]+\)$|rgba\(.*, [\d.]+\)$/.test(getComputedStyle(bg).backgroundColor)) bg = bg.parentElement; // skip translucent tints: the opaque layer below sets the ratio
  return [getComputedStyle(el).color, bg ? getComputedStyle(bg).backgroundColor : 'rgb(0, 0, 0)'];
}

function summary() {
  const state = $('results').dataset.state;
  const d = st.loaded.get(st.selected);
  return {
    state,
    rows: document.querySelectorAll('.row').length,
    tapesDrawn: document.querySelectorAll('.row .tape').length,
    tapeTicks: document.querySelectorAll('.row .tape rect').length,
    litTicks: document.querySelectorAll('.row .tape rect.h').length,
    selectedProject: d?.project ?? null,
    title: $('session').hidden ? null : $('session').querySelector('h1')?.textContent ?? null,
    command: $('session').hidden ? null : $('session').querySelector('.cmd .mono')?.textContent ?? null,
    matches: d ? matchIdx(d).length : 0,
    mapTicks: document.querySelectorAll('#map rect').length,
    marks: document.querySelectorAll('#thread mark').length,
    errorText: document.querySelector('.msgbox.err p')?.textContent ?? null,
    busy: $('results').hasAttribute('aria-busy'),
    skeletons: document.querySelectorAll('#results .skel').length,
    alert: document.querySelectorAll('.msgbox.err[role=alert]').length,
    retry: !!$('retry'),
    readerNone: !$('none').hidden,
    sessionShown: !$('session').hidden,
    emptyText: document.querySelector('#results .msgbox h2')?.textContent ?? null,
    contrast: Object.fromEntries(['.word', '.rhead h1', '.msgbox h2', '#none'].map((q) => [q, colors(q)])),
    font: getComputedStyle(document.body).fontFamily,
    bodyColor: getComputedStyle(document.body).color,
    hidden: (() => { const r = document.querySelector('.visually-hidden')?.getBoundingClientRect(); return r ? [r.width, r.height] : null; })(),
    when: [...document.querySelectorAll('.row .when')].map((e) => e.textContent),
    count: document.querySelector('#results .count')?.textContent ?? null,
    chips: [...document.querySelectorAll('#read .chip')].map((c) => c.textContent),
  };
}

(async () => {
  const cfg = await invoke('e2e_config').catch(() => null);
  if (cfg) $('q').value = cfg.query;
  const p = refresh();
  await new Promise((r) => setTimeout(r, 300));
  const early = summary();
  await p;
  if (cfg) await invoke('e2e_report', { report: JSON.stringify({ ...summary(), early }) });
})();
