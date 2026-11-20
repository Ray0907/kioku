#!/usr/bin/env python3
"""Turn a `tmux capture-pane -e -p` dump into a standalone HTML terminal frame.

Handles the SGR subset kioku emits: 24-bit and 16/256 colours, bold, faint,
underline, reverse and resets. Usage: ansi2html.py <capture> <out.html> [bg] [fg]
"""
import html, re, sys, unicodedata

src, out = sys.argv[1], sys.argv[2]
BG = sys.argv[3] if len(sys.argv) > 3 else "#1c1d22"
FG = sys.argv[4] if len(sys.argv) > 4 else "#d4d6dc"
BASIC = ["#1c1d22", "#e06c75", "#98c379", "#e5c07b", "#61afef", "#c678dd", "#56b6c2", "#d4d6dc",
         "#5c6370", "#ff7b86", "#b5e890", "#ffd68a", "#7cc4ff", "#de8cff", "#6fd8e3", "#ffffff"]

def c256(n):
    if n < 16: return BASIC[n]
    if n < 232:
        n -= 16; v = [0, 95, 135, 175, 215, 255]
        return "#%02x%02x%02x" % (v[n // 36], v[n // 6 % 6], v[n % 6])
    g = 8 + (n - 232) * 10; return "#%02x%02x%02x" % (g, g, g)

def line_html(line):
    line = re.sub(r"\x1b\]8;[^\x07\x1b]*(?:\x07|\x1b\\)", "", line)  # OSC 8 links carry no pixels
    st = {"fg": None, "bg": None, "b": False, "dim": False, "u": False, "rev": False}
    parts, pos = [], 0
    def emit(text):
        if not text: return
        fg, bg = st["fg"] or FG, st["bg"]
        if st["rev"]: fg, bg = (bg or BG), fg
        css = [f"color:{fg}"]
        if bg: css.append(f"background:{bg}")
        if st["b"]: css.append("font-weight:700")
        if st["dim"]: css.append("opacity:.5")
        if st["u"]: css.append("text-decoration:underline;text-underline-offset:3px")
        # Pin every wide (CJK) glyph to exactly two cells so columns line up like a terminal.
        # Ambiguous-width box and arrow glyphs get one cell for the same reason.
        body = "".join(f'<i class="w">{html.escape(ch)}</i>' if unicodedata.east_asian_width(ch) in "WF"
                       else f'<i class="n">{html.escape(ch)}</i>' if "\u2190" <= ch <= "\u259f"
                       else html.escape(ch) for ch in text)
        parts.append(f'<span style="{";".join(css)}">{body}</span>')
    for m in re.finditer(r"\x1b\[([0-9;:]*)m", line):
        emit(line[pos:m.start()]); pos = m.end()
        ps = [int(p) if p.isdigit() else 0 for p in re.split("[;:]", m.group(1) or "0")]
        i = 0
        while i < len(ps):
            p = ps[i]
            if p == 0: st.update(fg=None, bg=None, b=False, dim=False, u=False, rev=False)
            elif p == 1: st["b"] = True
            elif p == 2: st["dim"] = True
            elif p == 22: st["b"] = st["dim"] = False
            elif p == 4: st["u"] = True
            elif p == 24: st["u"] = False
            elif p == 7: st["rev"] = True
            elif p == 27: st["rev"] = False
            elif p in (38, 48) and i + 1 < len(ps):
                key = "fg" if p == 38 else "bg"
                if ps[i + 1] == 2 and i + 4 < len(ps): st[key] = "#%02x%02x%02x" % tuple(ps[i + 2:i + 5]); i += 4
                elif ps[i + 1] == 5 and i + 2 < len(ps): st[key] = c256(ps[i + 2]); i += 2
            elif p == 39: st["fg"] = None
            elif p == 49: st["bg"] = None
            elif 30 <= p <= 37: st["fg"] = BASIC[p - 30]
            elif 90 <= p <= 97: st["fg"] = BASIC[p - 90 + 8]
            elif 40 <= p <= 47: st["bg"] = BASIC[p - 40]
            elif 100 <= p <= 107: st["bg"] = BASIC[p - 100 + 8]
            i += 1
    emit(line[pos:])
    return "".join(parts) or "&nbsp;"

lines = open(src, encoding="utf-8").read().rstrip("\n").split("\n")
body = "".join(f"<div>{line_html(l)}</div>" for l in lines)
open(out, "w", encoding="utf-8").write(f"""<!doctype html><meta charset="utf-8">
<style>
html,body{{margin:0;background:{BG}}}
.term{{display:inline-block;padding:22px 26px;background:{BG};color:{FG};
font:15px/1.5 "JetBrains Mono",Menlo,"PingFang TC","Hiragino Sans","Apple SD Gothic Neo",monospace;white-space:pre}}
.term div{{height:1.5em}}
.w{{display:inline-block;width:2ch;text-align:center;font-style:normal}}
.n{{display:inline-block;width:1ch;text-align:center;font-style:normal;overflow:visible}}
</style><div class="term">{body}</div>""")
