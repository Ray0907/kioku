// Pure helpers: escaping, dates, highlight marking, and the kioku process queue.
export const esc = (s) => s.replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]);
export const CJK = /[぀-ヿ㐀-鿿가-힯]/;
// ---------- small helpers ----------

export const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
export const fmtDate = (ymd) => {
  const [, m, d] = ymd.split('-').map(Number);
  return m ? `${d} ${MONTHS[m - 1]}` : ymd;
};
export const tz = (() => {
  const off = -new Date().getTimezoneOffset();
  const a = Math.abs(off);
  const hh = String(Math.floor(a / 60)).padStart(2, '0');
  const mm = a % 60 ? ':' + String(a % 60).padStart(2, '0') : '';
  return `${off < 0 ? '-' : '+'}${hh}${mm}`;
})();
export const who = (h) => ({ claude: 'Claude', codex: 'Codex', pi: 'Pi' })[h] ?? h.charAt(0).toUpperCase() + h.slice(1);

/** Wrap highlighted ranges (UTF-16 offsets, as kioku reports them) in <mark>. Text is escaped. */
export function marked(text, highlights) {
  if (!highlights?.length) return esc(text);
  let out = '';
  let at = 0;
  for (const h of [...highlights].sort((a, b) => a.location - b.location)) {
    if (h.location < at) continue;
    out += esc(text.slice(at, h.location)) + '<mark>' + esc(text.slice(h.location, h.location + h.length)) + '</mark>';
    at = h.location + h.length;
  }
  return out + esc(text.slice(at));
}

// At most three kioku processes at once; each `show` can be a whole long session.
export const queue = (() => {
  let running = 0;
  const waiting = [];
  const pump = () => {
    while (running < 3 && waiting.length) {
      running++;
      const { fn, resolve, reject } = waiting.shift();
      fn().then(resolve, reject).finally(() => { running--; pump(); });
    }
  };
  return (fn) => new Promise((resolve, reject) => { waiting.push({ fn, resolve, reject }); pump(); });
})();
