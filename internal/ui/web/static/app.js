// ebira web UI: notebooks, notes, search, a preview, an editor and questions
// to the assistant, over the REST API of the same server. Text from notes is put into the page with
// textContent; only the rendered Markdown body is HTML, sanitized with
// DOMPurify.
"use strict";

const state = {
  notebooks: [],
  trashed: [], // notebooks in the trash, deleted as a whole
  notebook: null, // selected notebook ID; null for all but the trash
  trashedNotebook: null, // with notebook "trash": the selected notebook in the trash, whose notes are listed
  tags: [], // tags with their note counts
  marks: [], // the marks a note can have, with their names (GET /marks)
  tag: null, // selected tag; the list then has the notes with it in all notebooks
  side: loadSide(), // what the left column lists: "notebooks" or "tags"
  query: "",
  notes: [],
  total: 0,
  selected: null, // ID of the note in the preview
  chosen: new Set(), // IDs of the notes chosen together with Cmd/Ctrl or Shift; two or more replace the preview
  anchor: null, // the note a Shift-click chooses from
  note: null, // the note in the preview
  editing: null, // the editor's state while editing; see startEdit
  allSort: loadAllSort(), // sort of all notes; a notebook keeps its own on the server
  want: 0, // number of notes of the list to show, a page more on "load more"
  expanded: loadExpanded(), // keys of the sub notebooks opened in the note list of their parent (see subKey)
  subNotes: new Map(), // key of an opened sub notebook: its notes {notes, total, want}
};

// loadExpanded reads the sub notebooks opened in the note list, kept per
// browser.
function loadExpanded() {
  try {
    return new Set(JSON.parse(localStorage.getItem("ebira.expanded") || "[]"));
  } catch (_) {
    return new Set();
  }
}

// The view, the notebook (or notebook in the trash), tag, search and note
// shown, is kept for the tab in sessionStorage, so that a reload comes back to
// it. It is saved when the list or the note changes, and restored at start.
function saveView() {
  const view = { notebook: state.notebook, trashedNotebook: state.trashedNotebook, tag: state.tag, query: state.query, selected: state.selected };
  try { sessionStorage.setItem("ebira.view", JSON.stringify(view)); } catch (_) { /* not kept */ }
}

// restoreView brings back the list of the view kept, as far as its notebook
// or tag still exists, and returns the note that was shown.
function restoreView() {
  let view;
  try {
    view = JSON.parse(sessionStorage.getItem("ebira.view") || "null");
  } catch (_) {
    return null;
  }
  if (!view) return null;
  if (view.notebook === "trash" ? !view.trashedNotebook || state.trashed.some((n) => n.id === view.trashedNotebook)
    : !view.notebook || state.notebooks.some((n) => n.id === view.notebook)) {
    state.notebook = view.notebook ?? null;
    state.trashedNotebook = view.notebook === "trash" ? view.trashedNotebook ?? null : null;
  }
  if (view.tag && !state.notebook && state.tags.some((tg) => tg.name === view.tag)) state.tag = view.tag;
  if (typeof view.query === "string" && view.query) {
    state.query = view.query;
    $("q").value = view.query;
    showSearchClear();
  }
  return typeof view.selected === "string" ? view.selected : null;
}

function saveExpanded() {
  try { localStorage.setItem("ebira.expanded", JSON.stringify([...state.expanded])); } catch (_) { /* this page only */ }
}

// LIST_PAGE is the number of notes read at a time, the most the API allows.
const LIST_PAGE = 500;

// The sort of all notes is a per-browser convenience; a notebook's sort is in
// its settings file, shared through git.
function loadAllSort() {
  try {
    return localStorage.getItem("ebira.allSort") || "order";
  } catch (_) {
    return "order";
  }
}

// The left column lists the notebooks or the tags, as in Quiver; the choice is
// a per-browser convenience.
function loadSide() {
  try {
    return localStorage.getItem("ebira.side") === "tags" ? "tags" : "notebooks";
  } catch (_) {
    return "notebooks";
  }
}

function setSide(side) {
  state.side = side;
  try {
    localStorage.setItem("ebira.side", side);
  } catch (_) { /* kept for this page only */ }
  renderNotebooks();
}

function currentSort() {
  if (!state.notebook) return state.allSort;
  return state.notebooks.find((n) => n.id === state.notebook)?.sort || "order";
}

// canReorder reports whether the notes shown can be moved by hand: those of a
// notebook in the manual order, without a search.
function canReorder() {
  return !state.query && !!state.notebook && state.notebook !== "trash" && currentSort() === "order";
}

// Sub notebooks open in the note list of their parent, as an accordion: a
// click on the row of one shows its notes right under it, and another click
// folds them. subKey names one, in the trash or not.
const subKey = (id, trashed) => (trashed ? "trash:" : "") + id;

// openSubs returns the sub notebooks opened in the list shown, in the order
// of their rows: {key, sub, trashed}. None with a search, a tag, or a sub
// notebook listed alone.
function openSubs() {
  if (state.query || state.tag || !state.notebook) return [];
  const trashed = state.notebook === "trash";
  const cur = trashed ? state.trashedNotebook : state.notebook;
  if ((!cur && !trashed) || (cur && parentOf(cur, trashed))) return [];
  const opened = (sub) => state.expanded.has(subKey(sub.id, trashed));
  return childNotebooks(cur, trashed).filter(opened).flatMap((sub) => [
    // In the trash itself, an opened notebook shows its sub notebooks in the
    // trash, whose notes come before its own.
    ...(trashed && !cur ? childNotebooks(sub.id, true).filter(opened) : []),
    sub,
  ]).map((sub) => ({ key: subKey(sub.id, trashed), sub, trashed }));
}

// childNotebooks returns the notebooks shown as rows in the note list of
// notebook cur: its sub notebooks, or, for the trash itself (cur null in the
// trash), the notebooks deleted as a whole.
function childNotebooks(cur, trashed) {
  return (trashed ? state.trashed : state.notebooks).filter((n) => (cur ? n.parent === cur : !n.parent));
}

// listedNotes returns the notes of the list in the order shown: those of the
// opened sub notebooks under their rows at the top, then the notebook's own.
function listedNotes() {
  return [...openSubs().flatMap((x) => state.subNotes.get(x.key)?.notes ?? []), ...state.notes];
}

const findListed = (id) => listedNotes().find((n) => n.id === id);

// listOf returns the list note id is in, with whether its notes can be moved
// by hand: those of an opened sub notebook, or the notebook's own.
function listOf(id) {
  for (const x of openSubs()) {
    const notes = state.subNotes.get(x.key)?.notes ?? [];
    if (notes.some((n) => n.id === id)) return { notes, reorder: !x.trashed && (x.sub.sort || "order") === "order" };
  }
  return { notes: state.notes, reorder: canReorder() };
}

// readSubNotes reads the notes of the opened sub notebook x, in its own sort,
// as many pages as shown before.
async function readSubNotes(x) {
  const want = state.subNotes.get(x.key)?.want || LIST_PAGE;
  const notes = [];
  let total = 0;
  do {
    const params = x.trashed ? { notebook: "trash", trashed_notebook: x.sub.id } : { notebook: x.sub.id };
    const r = await api("/notes", { params: { ...params, limit: LIST_PAGE, offset: notes.length } });
    notes.push(...r.notes);
    total = r.total;
    if (!r.notes.length) break;
  } while (notes.length < Math.min(want, total));
  return { notes, total, want };
}

// toggleSub opens or folds the sub notebook key in the note list.
function toggleSub(key) {
  if (state.expanded.has(key)) {
    state.expanded.delete(key);
    saveExpanded();
    renderNotes();
    return;
  }
  state.expanded.add(key);
  saveExpanded();
  loadNotes();
}

const $ = (id) => document.getElementById(id);

// Column widths: the resizers between the columns set the widths, kept per
// browser. Double-click or Enter puts a column back to its default width.
const COLUMNS = { side: { prop: "--w-side", min: 170 }, list: { prop: "--w-list", min: 200 }, ask: { prop: "--w-ask", min: 260 } };
const PREVIEW_MIN = 280; // the preview keeps this much while a column grows

function columnPane(col) {
  return { side: document.querySelector(".pane.notebooks"), list: document.querySelector(".pane.list"), ask: $("assistant") }[col];
}

function setColumnWidth(col, px) {
  const pane = columnPane(col);
  const room = $("preview").getBoundingClientRect().width - PREVIEW_MIN;
  const max = pane.getBoundingClientRect().width + Math.max(0, room);
  const w = Math.round(Math.max(COLUMNS[col].min, Math.min(max, px)));
  document.querySelector(".panes").style.setProperty(COLUMNS[col].prop, `${w}px`);
}

function savedColumnWidths() {
  try {
    return JSON.parse(localStorage.getItem("ebira.columns") || "{}") || {};
  } catch (_) {
    return {}; // the defaults
  }
}

function loadColumnWidths() {
  const saved = savedColumnWidths();
  for (const [col, { prop, min }] of Object.entries(COLUMNS)) {
    document.querySelector(".panes").style.removeProperty(prop);
    if (Number.isFinite(saved[col])) document.querySelector(".panes").style.setProperty(prop, `${Math.max(min, saved[col])}px`);
  }
}

// saveColumnWidth keeps the width of col as it is now, or forgets it when it
// has the default width. Only the column moved is kept: the others may have
// been narrowed for the room.
function saveColumnWidth(col) {
  const saved = savedColumnWidths();
  const v = document.querySelector(".panes").style.getPropertyValue(COLUMNS[col].prop);
  if (v) saved[col] = parseFloat(v);
  else delete saved[col];
  try {
    localStorage.setItem("ebira.columns", JSON.stringify(saved));
  } catch (_) { /* kept for this page only */ }
}

// fitColumns puts back the saved widths, then narrows the columns, the list
// first, while the preview has less than PREVIEW_MIN, such as when the
// assistant opens or the window narrows. The narrowed widths are not saved, so
// the chosen ones come back when there is room again.
function fitColumns() {
  loadColumnWidths();
  if (getComputedStyle(document.querySelector(".resizer")).display === "none") return; // one column
  // The columns may be wider than the window in all, leaving the preview no
  // width at all: count from the width of the window.
  const width = (col) => (columnPane(col).hidden ? 0 : columnPane(col).getBoundingClientRect().width);
  let short = PREVIEW_MIN - (document.querySelector(".panes").clientWidth - width("side") - width("list") - width("ask"));
  for (const col of ["list", "ask", "side"]) {
    if (short <= 0) return;
    if (columnPane(col).hidden) continue;
    const w = width(col);
    const cut = Math.min(short, w - COLUMNS[col].min);
    if (cut <= 0) continue;
    document.querySelector(".panes").style.setProperty(COLUMNS[col].prop, `${Math.round(w - cut)}px`);
    short -= cut;
  }
}

function resetColumnWidth(col) {
  document.querySelector(".panes").style.removeProperty(COLUMNS[col].prop);
  saveColumnWidth(col);
}

for (const r of document.querySelectorAll(".resizer")) {
  const col = r.dataset.col;
  // The assistant is right of its resizer: moving the resizer right narrows it.
  const sign = col === "ask" ? -1 : 1;
  r.addEventListener("pointerdown", (e) => {
    if (e.button !== 0) return;
    e.preventDefault();
    const x = e.clientX, w = columnPane(col).getBoundingClientRect().width;
    r.setPointerCapture(e.pointerId);
    r.classList.add("dragging");
    document.body.classList.add("resizing");
    const move = (e) => setColumnWidth(col, w + sign * (e.clientX - x));
    const end = () => {
      r.removeEventListener("pointermove", move);
      r.removeEventListener("pointerup", end);
      r.removeEventListener("pointercancel", end);
      r.classList.remove("dragging");
      document.body.classList.remove("resizing");
      saveColumnWidth(col);
    };
    r.addEventListener("pointermove", move);
    r.addEventListener("pointerup", end);
    r.addEventListener("pointercancel", end);
  });
  r.addEventListener("dblclick", () => resetColumnWidth(col));
  r.addEventListener("keydown", (e) => {
    const step = e.shiftKey ? 64 : 16;
    if (e.key === "ArrowLeft" || e.key === "ArrowRight") {
      e.preventDefault();
      e.stopPropagation(); // not the keys of the list
      const w = columnPane(col).getBoundingClientRect().width;
      setColumnWidth(col, w + sign * (e.key === "ArrowRight" ? step : -step));
      saveColumnWidth(col);
    } else if (e.key === "Enter") {
      e.preventDefault();
      e.stopPropagation();
      resetColumnWidth(col);
    }
  });
}
fitColumns();
window.addEventListener("resize", fitColumns);

// Messages: the text of the UI is in i18n/<lang>.json, keyed by name, so that
// the code holds no text of a language. {name} in a message is replaced with
// the value of name. English is the base: a message missing in the language
// is shown in English.

const LANGS = ["en", "ja"]; // as web.Langs
let messages = {};
let lang = "en";

// chooseLang returns the language of the UI: the one chosen in the settings
// menu, else the one of the server (the lang attribute of the page, from the
// settings file or -lang), else the first of the browser's languages that the
// UI has, else English.
function chooseLang() {
  let chosen = null;
  try { chosen = localStorage.getItem("ebira.lang"); } catch (_) { /* none */ }
  const wanted = [chosen, document.documentElement.getAttribute("lang"), ...(navigator.languages || [navigator.language])];
  for (const w of wanted) {
    const l = (w || "").toLowerCase().split("-")[0];
    if (LANGS.includes(l)) return l;
  }
  return "en";
}

async function fetchMessages(l) {
  try {
    const res = await fetch(`assets/i18n/${l}.json`);
    if (res.ok) return await res.json();
  } catch (_) { /* the keys are shown instead */ }
  return {};
}

function t(key, vars) {
  const s = messages[key] ?? key;
  return vars ? s.replace(/\{(\w+)\}/g, (m, k) => (k in vars ? String(vars[k]) : m)) : s;
}

async function loadMessages() {
  lang = chooseLang();
  document.documentElement.lang = lang;
  const all = Object.fromEntries(await Promise.all(LANGS.map(async (l) => [l, await fetchMessages(l)])));
  messages = { ...all.en, ...all[lang] };
  // Each language is named in itself in the settings menu.
  for (const b of document.querySelectorAll("#settings-menu [data-lang]")) {
    b.textContent = all[b.dataset.lang].langName || b.dataset.lang;
    b.lang = b.dataset.lang;
    b.setAttribute("aria-checked", String(b.dataset.lang === lang));
  }
  // The page marks its text with data-i18n (the text), data-i18n-title,
  // data-i18n-placeholder and data-i18n-aria-label.
  for (const e of document.querySelectorAll("[data-i18n]")) e.textContent = t(e.dataset.i18n);
  for (const [attr, name] of [["title", "i18nTitle"], ["placeholder", "i18nPlaceholder"], ["aria-label", "i18nAriaLabel"]]) {
    for (const e of document.querySelectorAll(`[data-${attr === "aria-label" ? "i18n-aria-label" : "i18n-" + attr}]`)) e.setAttribute(attr, t(e.dataset[name]));
  }
  // The search box tells the search mode, which the server sets in the page.
  const mode = document.documentElement.dataset.searchMode;
  if (mode) $("q").placeholder = t("searchPlaceholderMode", { mode });
}

// API

// api calls the REST API. body is sent as JSON; raw, such as a File, as is.
async function api(path, opts = {}) {
  const { method = "GET", params, body, raw, ifMatch, signal } = opts;
  const url = new URL(path, location.origin);
  for (const [k, v] of Object.entries(params || {})) {
    if (v !== null && v !== undefined && v !== "") url.searchParams.set(k, v);
  }
  const headers = { Accept: "application/json" };
  if (body !== undefined) headers["Content-Type"] = "application/json";
  if (raw !== undefined) headers["Content-Type"] = "application/octet-stream";
  if (ifMatch) headers["If-Match"] = ifMatch;
  const payload = raw !== undefined ? raw : body === undefined ? undefined : JSON.stringify(body);
  const res = await fetch(url, { method, headers, body: payload, signal });
  if (!res.ok) {
    let msg = `${res.status} ${res.statusText}`;
    let name = "";
    try {
      const e = await res.json();
      if (e.message) msg = e.message;
      name = e.name || "";
    } catch (_) { /* not JSON */ }
    // An important operation asks to sign in with the passkey again: ask,
    // then try once more.
    if (name === "verification_required" && !opts.verified) {
      await verifyPasskey();
      return api(path, { ...opts, verified: true });
    }
    const err = new Error(msg);
    err.status = res.status;
    err.name = name || "Error";
    // An invalid or expired session: sign in again.
    if (res.status === 401 && !url.pathname.startsWith("/auth/")) showSignIn(t("authExpired"));
    throw err;
  }
  if (res.status === 204) return null;
  const data = await res.json();
  // Over HTTP the version of a note comes in the ETag header, not the body.
  const etag = res.headers.get("ETag");
  if (etag && data && typeof data === "object" && !Array.isArray(data)) data.version = etag;
  return data;
}

const noteURL = (id) => `/notes/${encodeURIComponent(id)}`;

// Highlights: the API gives ranges in Unicode code points, and JavaScript
// strings index UTF-16 code units.

function toUnits(text, ranges) {
  const offsets = [0]; // offsets[i] is the code unit offset of code point i
  for (const ch of text) offsets.push(offsets[offsets.length - 1] + ch.length);
  const at = (i) => offsets[Math.min(Math.max(i, 0), offsets.length - 1)];
  return (ranges || []).map((r) => [at(r.start), at(r.end)]);
}

// highlighted returns a fragment of text with the ranges in <mark>.
function highlighted(text, ranges) {
  const frag = document.createDocumentFragment();
  let pos = 0;
  for (const [s, e] of toUnits(text, ranges)) {
    if (s < pos || e <= s) continue;
    frag.append(text.slice(pos, s));
    const m = document.createElement("mark");
    m.textContent = text.slice(s, e);
    frag.append(m);
    pos = e;
  }
  frag.append(text.slice(pos));
  return frag;
}

// Markers put around the highlighted ranges of the Markdown source before
// rendering, then turned into <mark> elements. They are private use
// characters, which notes do not contain in practice.
const MARK_OPEN = "\uE000";
const MARK_CLOSE = "\uE001";
const MARKERS_IN_ATTR = /\uE000|\uE001|%EE%80%8[01]/gi;

function markSource(text, ranges) {
  let out = "";
  let pos = 0;
  for (const [s, e] of toUnits(text, ranges)) {
    if (s < pos || e <= s) continue;
    out += text.slice(pos, s) + MARK_OPEN + text.slice(s, e) + MARK_CLOSE;
    pos = e;
  }
  return out + text.slice(pos);
}

// noteLink returns the note ID of a link to a note in this UI (/#note/<ID>,
// or /ui/#note/<ID> where the UI was before), which the assistant puts in its
// answers, or null.
function noteLink(href) {
  let u;
  try {
    u = new URL(href, location.href);
  } catch (_) {
    return null;
  }
  if (u.origin !== location.origin || (u.pathname !== "/" && u.pathname !== "/ui/")) return null;
  const m = /^#note\/(.+)$/.exec(u.hash);
  if (!m) return null;
  try {
    return decodeURIComponent(m[1]);
  } catch (_) {
    return null;
  }
}

// FILE_PATH matches the path of a file of a note as the body refers to it,
// relative to the note file: <id>.assets/<name>.
const FILE_PATH = /^([a-z0-9][a-z0-9_-]{0,63})\.assets\/([^/?#]+)$/;

// fileURL returns the API URL of the file a body refers to with path, or null
// for another path.
function fileURL(path) {
  const m = FILE_PATH.exec(path.replace(MARKERS_IN_ATTR, ""));
  if (!m) return null;
  let name;
  try {
    name = decodeURIComponent(m[2]); // marked percent-encodes
  } catch (_) {
    return null;
  }
  return `${noteURL(m[1])}/files/${encodeURIComponent(name)}`;
}

DOMPurify.addHook("afterSanitizeAttributes", (node) => {
  // Links to notes open in this page; the others in a new tab.
  if (node.tagName === "A" && node.hasAttribute("href") && !noteLink(node.getAttribute("href"))) {
    node.setAttribute("target", "_blank");
    node.setAttribute("rel", "noopener noreferrer");
  }
  // The files of notes are served by the API.
  if (node.tagName === "IMG" && node.hasAttribute("src")) {
    const url = fileURL(node.getAttribute("src"));
    if (url) node.setAttribute("src", url);
  }
});

// Math and diagrams. The Markdown renderer leaves placeholders with the
// source in data-src: $...$ and $$...$$ (outside code) and ```math blocks for
// KaTeX, ```mermaid blocks for Mermaid. They are filled in after sanitizing,
// loading the libraries only when a note needs them.

// The source is percent-encoded: the sanitizer drops attributes that look
// like markup, such as Mermaid's dashed arrow -->.
const placeholder = (tag, cls, src) => `<${tag} class="${cls}" data-src="${encodeURIComponent(src)}"></${tag}>`;

marked.use({
  extensions: [
    {
      name: "mathBlock",
      level: "block",
      start: (src) => src.match(/^\$\$/m)?.index,
      tokenizer(src) {
        const m = /^\$\$([\s\S]+?)\$\$[ \t]*(?:\n|$)/.exec(src);
        if (m) return { type: "mathBlock", raw: m[0], text: m[1].trim() };
      },
      renderer: (tok) => placeholder("div", "math math-display", tok.text) + "\n",
    },
    {
      name: "mathInline",
      level: "inline",
      start: (src) => src.indexOf("$"),
      // As in Pandoc: no space just inside the dollars, and no digit right
      // after the closing one, so that prices such as $5 and $10 stay text.
      tokenizer(src) {
        const m = /^\$(?![\s$])((?:\\.|[^\\$\n])*?[^\s\\])\$(?!\d)/.exec(src);
        if (m) return { type: "mathInline", raw: m[0], text: m[1] };
      },
      renderer: (tok) => placeholder("span", "math", tok.text),
    },
  ],
  renderer: {
    code(tok) {
      if (tok.lang === "math") return placeholder("div", "math math-display", tok.text) + "\n";
      if (tok.lang === "mermaid") return placeholder("div", "mermaid", tok.text) + "\n";
      return false;
    },
  },
});

// loadScript loads a vendored script once.
const loaded = {};
function loadScript(src, css) {
  if (!loaded[src]) {
    if (css) document.head.append(Object.assign(document.createElement("link"), { rel: "stylesheet", href: css }));
    loaded[src] = new Promise((resolve, reject) => {
      const s = Object.assign(document.createElement("script"), { src, onload: resolve, onerror: () => reject(new Error(`cannot load ${src}`)) });
      document.head.append(s);
    });
  }
  return loaded[src];
}

function sourceOf(node) {
  let src = node.dataset.src || "";
  try {
    src = decodeURIComponent(src);
  } catch (_) { /* left as is */ }
  return src.replace(/\uE000|\uE001/g, ""); // highlight markers
}

async function renderMath(el) {
  const nodes = el.querySelectorAll(".math[data-src]");
  if (!nodes.length) return;
  try {
    await loadScript("assets/vendor/katex/katex.min.js", "assets/vendor/katex/katex.min.css");
  } catch (_) {
    return; // the source stays hidden; nothing better to show
  }
  for (const node of nodes) {
    // katex.render builds the nodes with the DOM, whose styles the CSP allows.
    katex.render(sourceOf(node), node, { displayMode: node.classList.contains("math-display"), throwOnError: false });
  }
}

let mermaidReady = null;
let diagramSeq = 0;
let diagramQueue = Promise.resolve();

// renderDiagrams renders the diagrams in el. Mermaid cannot render two
// diagrams at once, so the calls take turns.
function renderDiagrams(el) {
  if (!el.querySelector(".mermaid[data-src]")) return;
  diagramQueue = diagramQueue.then(() => drawDiagrams(el), () => drawDiagrams(el));
}

// isDark reports whether the page shows the dark theme: chosen, or the
// system's dark mode when none is (see theme.js).
function isDark() {
  const theme = document.documentElement.dataset.theme;
  return theme ? theme === "dark" : matchMedia("(prefers-color-scheme: dark)").matches;
}

async function drawDiagrams(el) {
  const nodes = [...el.querySelectorAll(".mermaid[data-src]")].filter((n) => n.isConnected && !n.firstChild);
  if (!nodes.length) return;
  try {
    mermaidReady ||= loadScript("assets/vendor/mermaid.min.js").then(() => mermaid.initialize({
      startOnLoad: false,
      securityLevel: "strict",
      htmlLabels: false,
      theme: isDark() ? "dark" : "default",
    }));
    await mermaidReady;
  } catch (_) {
    return;
  }
  for (const node of nodes) {
    const src = sourceOf(node);
    const id = `diagram-${++diagramSeq}`;
    try {
      // parse throws on a syntax error; render would also draw an error
      // diagram into the page.
      await mermaid.parse(src);
      const { svg } = await mermaid.render(id, src);
      // As an image, the SVG keeps its own styles, which the CSP would block
      // in the page, and can run nothing.
      const img = el.ownerDocument.createElement("img");
      img.src = "data:image/svg+xml;charset=utf-8," + encodeURIComponent(svg);
      img.alt = src;
      node.replaceChildren(img);
    } catch (e) {
      const pre = document.createElement("pre");
      pre.className = "diagram-error";
      pre.textContent = `${src}\n\n${e.message || e}`;
      node.replaceChildren(pre);
    } finally {
      // render leaves its working element in the page when it fails.
      document.getElementById(`d${id}`)?.remove();
      document.getElementById(id)?.remove();
    }
  }
}

// renderBody renders the Markdown body into el, with the ranges highlighted.
function renderBody(el, body, ranges) {
  // A single newline is a line break, as in GitHub comments and Quiver.
  const html = marked.parse(markSource(body, ranges), { gfm: true, breaks: true });
  // No CSS from notes: the CSP allows inline styles for KaTeX and Mermaid.
  el.innerHTML = DOMPurify.sanitize(html, { FORBID_TAGS: ["style"], FORBID_ATTR: ["style"] });
  renderMath(el);
  renderDiagrams(el);
  if (!ranges || !ranges.length) return;
  // Markers that ended up in attributes, such as a highlighted URL, which
  // marked percent-encodes, are dropped; those in text become <mark> elements.
  for (const node of el.querySelectorAll("*")) {
    for (const a of node.attributes) {
      const v = a.value.replace(MARKERS_IN_ATTR, "");
      if (v !== a.value) a.value = v;
    }
  }
  const walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT);
  const texts = [];
  while (walker.nextNode()) texts.push(walker.currentNode);
  let open = false; // a range may span text nodes
  for (const node of texts) {
    const s = node.nodeValue;
    if (!open && !s.includes(MARK_OPEN) && !s.includes(MARK_CLOSE)) continue;
    const frag = document.createDocumentFragment();
    let buf = "";
    const flush = () => {
      if (!buf) return;
      if (open) {
        const m = document.createElement("mark");
        m.textContent = buf;
        frag.append(m);
      } else {
        frag.append(buf);
      }
      buf = "";
    };
    for (const ch of s) {
      if (ch === MARK_OPEN) { flush(); open = true; }
      else if (ch === MARK_CLOSE) { flush(); open = false; }
      else buf += ch;
    }
    flush();
    node.replaceWith(frag);
  }
}

// Formatting

// kindIcon is the emoji of a notebook kind, for the options of a select, which
// hold text only; the other places draw it with kindSVG.
const kindIcon = (kind) => ({ collab: "👥", drop: "📥", private: "🔒" })[kind] || "";

// KIND_SVG holds the line drawings of the notebook kinds for the notebook
// column, its menu and the dialog of a new notebook, in a 24 by 24 box: two
// people for collab, a tray with an arrow into it for drop and a lock for
// private. They take the color of the text, so they read alike in the light
// and the dark theme.
const KIND_SVG = {
  collab: ["circle:9,8,3.2", "M3 20c0-3.3 2.7-6 6-6s6 2.7 6 6", "circle:16.5,8.5,2.6", "M16.5 14c2.6.3 4.5 2.6 4.5 5.5"],
  drop: ["M12 3v8", "M8.5 7.5 12 11l3.5-3.5", "M4 14h4.5l1.5 2.5h4l1.5-2.5H20", "M4 14v5a1 1 0 0 0 1 1h14a1 1 0 0 0 1-1v-5"],
  private: ["rect:5,11,14,10,2", "M8 11V7.5a4 4 0 0 1 8 0V11"],
  // Not a kind: the trash, which has none, shows a can in the notebook column.
  trash: ["M4 7h16", "M9.5 7V4.5h5V7", "M6.5 7l1 13h9l1-13", "M10 11v5.5", "M14 11v5.5"],
  // Not kinds either: a sub notebook in the note list, and the way back up.
  folder: ["M3.5 7A1.5 1.5 0 0 1 5 5.5h4l2 2h8A1.5 1.5 0 0 1 20.5 9v8.5A1.5 1.5 0 0 1 19 19H5a1.5 1.5 0 0 1-1.5-1.5z"],
  up: ["M12 19V6", "M6.5 11.5L12 6l5.5 5.5"],
  // The mark of a sub notebook that opens in the list, turned down when open.
  chevron: ["M9 5.5l6.5 6.5L9 18.5"],
  // And the buttons of the preview: copy the title (two sheets), copy the
  // note (a page with a folded corner and lines), edit (a pencil) and history
  // (a clock with an arrow turning back); the trash is the can above.
  // Copy a link to the note: two links of a chain.
  link: ["M10 13.5a4.5 4.5 0 0 0 6.4 0l3-3a4.5 4.5 0 0 0-6.4-6.4l-1.2 1.2", "M14 10.5a4.5 4.5 0 0 0-6.4 0l-3 3a4.5 4.5 0 0 0 6.4 6.4l1.2-1.2"],
  copy: ["rect:9,9,11,11,2", "M15 9V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v7a2 2 0 0 0 2 2h3"],
  document: ["M6 3h8l4 4v13a1 1 0 0 1-1 1H6a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1z", "M14 3v4h4", "M8.5 12h7", "M8.5 16h7"],
  // Find in the note: a magnifier.
  find: ["circle:10.5,10.5,6", "M15 15l5 5"],
  // Marks: a flag.
  flag: ["M5.5 21V4", "M5.5 4.5h11l-2.5 4 2.5 4h-11"],
  edit: ["M4 20h4L19 9l-4-4L4 16z", "M13.5 6.5l4 4"],
  history: ["M3.5 12a8.5 8.5 0 1 0 2.5-6", "M3.5 4v4.5H8", "M12 7.5V12l3 2"],
};

// iconButton returns a button that shows the drawing of KIND_SVG named icon,
// named by label for the tooltip and screen readers.
function iconButton(icon, label, onClick) {
  const b = button(kindSVG(icon), onClick, { title: label, cls: "icon-button" });
  b.setAttribute("aria-label", label);
  return b;
}

// kindSVG returns the drawing of a notebook kind, or null for the default kind.
function kindSVG(kind) {
  const shapes = KIND_SVG[kind];
  if (!shapes) return null;
  const ns = "http://www.w3.org/2000/svg";
  const svg = document.createElementNS(ns, "svg");
  for (const [k, v] of Object.entries({ viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", "stroke-width": "1.8", "stroke-linecap": "round", "stroke-linejoin": "round", "aria-hidden": "true" })) {
    svg.setAttribute(k, v);
  }
  for (const s of shapes) {
    const [type, args] = s.includes(":") ? s.split(":") : ["path", s];
    const e = document.createElementNS(ns, type);
    const n = args.split(",");
    if (type === "path") e.setAttribute("d", args);
    if (type === "circle") [["cx", n[0]], ["cy", n[1]], ["r", n[2]]].forEach(([a, x]) => e.setAttribute(a, x));
    if (type === "rect") [["x", n[0]], ["y", n[1]], ["width", n[2]], ["height", n[3]], ["rx", n[4]]].forEach(([a, x]) => e.setAttribute(a, x));
    svg.append(e);
  }
  return svg;
}

// folderSVG returns the drawing of a folder with the drawing of a notebook
// kind drawn small inside it.
function folderSVG(kind) {
  const svg = kindSVG("folder");
  const mark = kindSVG(kind);
  const g = document.createElementNS("http://www.w3.org/2000/svg", "g");
  // The kind drawing, 24 units wide, scaled into the body of the folder
  // (7.5 to 19 high), with its strokes kept as thick as they look elsewhere.
  g.setAttribute("transform", "translate(7.2 8.4) scale(.4)");
  g.setAttribute("stroke-width", "3.6");
  g.append(...mark.childNodes);
  svg.append(g);
  return svg;
}

// aiMark returns the mark of a note created by an AI client, which goes at the
// end of a row rather than right after the title: sparkles, as apps mark what
// AI made.
const AI_MARK = "\u2728";

function aiMark(note, label) {
  if (note.created_by !== "ai") return null;
  const m = el("span", "ai-mark", label ? `${AI_MARK} ${label}` : AI_MARK);
  m.title = t("aiNote");
  return m;
}

// notebookName is the display name of notebook id, with the name of its
// parent for a sub notebook.
function notebookName(id) {
  const nb = state.notebooks.find((n) => n.id === id);
  if (!nb) return id;
  return nb.parent ? notebookName(nb.parent) + " / " + nb.name : nb.name;
}

// trashedName is the display name of a notebook in the trash, with the name
// of its parent for a sub notebook deleted with it.
function trashedName(id) {
  const tn = state.trashed.find((n) => n.id === id);
  if (!tn) return id;
  return tn.parent ? trashedName(tn.parent) + " / " + tn.name : tn.name;
}

// nbPath is notebook id in the path of a URL: a sub notebook keeps the slash
// between its parent and itself, which the API takes as one.
function nbPath(id) {
  return id.split("/").map(encodeURIComponent).join("/");
}

// parentOf returns the parent of notebook id, or of notebook id in the trash
// with trashed; undefined for one that is not a sub notebook.
function parentOf(id, trashed = false) {
  return (trashed ? state.trashed : state.notebooks).find((n) => n.id === id)?.parent;
}

// trashedID returns the ID that notebook nb has in the trash once deleted on
// its own: a sub notebook is <parent>~<sub>.
function trashedID(nb) {
  return nb.parent ? nb.parent + "~" + nb.id.slice(nb.parent.length + 1) : nb.id;
}

// formatTime formats the time s in the language of the page, to the minute,
// or to the second with seconds.
function formatTime(s, seconds) {
  const d = new Date(s);
  if (isNaN(d)) return s;
  return d.toLocaleString(lang, { year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", second: seconds ? "2-digit" : undefined });
}

function el(tag, cls, ...children) {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  e.append(...children);
  return e;
}

function button(label, onClick, { title, cls } = {}) {
  const b = el("button", cls || "", label);
  b.type = "button";
  if (title) b.title = title;
  b.addEventListener("click", onClick);
  return b;
}

// tagList returns the tags of a note; with link, a click on one chooses it.
function tagList(tags, link = false) {
  return (tags || []).map((tag) => {
    const e = el("span", link ? "tag link" : "tag", tag);
    if (link) {
      e.title = t("showTag", { tag });
      e.addEventListener("click", () => selectTag(tag));
    }
    return e;
  });
}

// Notebooks

// loadNotebooks reads the notebooks and the tags, which change together with
// the notes.
async function loadNotebooks() {
  const [notebooks, tags, trashed, marks] = await Promise.all([api("/notebooks"), api("/tags"), api("/trash/notebooks"), api("/marks").catch(() => [])]);
  state.notebooks = notebooks;
  state.marks = marks;
  state.tags = tags;
  state.trashed = trashed;
  notebookTags = { notebook: null, tags: [] }; // read again when offered
  if (state.notebook && !state.notebooks.some((n) => n.id === state.notebook)) {
    state.notebook = null; // deleted
  }
  if (state.trashedNotebook && !state.trashed.some((n) => n.id === state.trashedNotebook)) {
    state.trashedNotebook = null; // restored or deleted
  }
  renderNotebooks();
}

// withSubs returns the count of notebook nb of list with the notes of its sub
// notebooks, which the left column does not show, so that the counts there
// add up to that of all notes.
const withSubs = (list, nb) => list.reduce((a, n) => (n.parent === nb.id ? a + n.count : a), nb.count);

// renderNotebooks renders the left column: the notebooks or the tags, each
// below "all notes".
function renderNotebooks() {
  if (renaming) return; // drawn again when the new name is saved or dropped
  for (const b of document.querySelectorAll("#side-tabs button")) {
    b.setAttribute("aria-selected", String(b.dataset.side === state.side));
  }
  const ul = $("notebooks");
  ul.replaceChildren();
  const total = state.notebooks.filter((n) => n.id !== "trash").reduce((a, n) => a + n.count, 0);
  const all = el("li", !state.tag && !state.notebook ? "active" : "", el("span", "icon"), el("span", "name", t("all")), el("span", "count", String(total)));
  all.addEventListener("click", () => selectNotebook(null));
  ul.append(all);
  if (state.side === "tags") {
    renderTags(ul);
    return;
  }
  const item = (id, name, kind, count, cls, trashed = null) => {
    // The icon has a column of its own before the name, kept even without an
    // icon, so that the names line up.
    const li = el("li", cls || "");
    if (trashed) li.append(el("span", "nest", "↳"));
    const icon = el("span", "icon");
    const svg = kindSVG(id === "trash" && !trashed ? "trash" : kind);
    if (svg) icon.append(svg);
    if (svg && KINDS.includes(kind) && id !== "trash") icon.title = kind;
    li.append(icon, el("span", "name", name), el("span", "count", String(count)));
    // A sub notebook is opened from the note list; its parent stays marked,
    // as the trash does for a notebook in it.
    const active = trashed
      ? state.notebook === "trash" && (state.trashedNotebook === trashed || parentOf(state.trashedNotebook, true) === trashed)
      : id === "trash" ? state.notebook === "trash" : state.trashedNotebook === null && (state.notebook === id || parentOf(state.notebook) === id);
    if (!state.tag && active) li.classList.add("active");
    li.addEventListener("click", () => selectNotebook(id, trashed));
    li.addEventListener("contextmenu", (e) => {
      e.preventDefault();
      openNotebookMenu(li, id, trashed, e);
    });
    const more = button("…", (e) => {
      e.stopPropagation();
      openNotebookMenu(li, id, trashed);
    }, { cls: "nb-more", title: t("notebookActions") });
    more.setAttribute("aria-haspopup", "menu");
    more.setAttribute("aria-label", t("notebookActions"));
    li.append(more);
    li.dataset.notebook = id;
    if (!trashed) {
      acceptImport(li, id);
      acceptNote(li, id);
    }
    return li;
  };
  for (const nb of state.notebooks) {
    if (nb.parent) continue; // in the note list of its parent
    // The notebooks in the trash are in its note list, as sub notebooks are
    // in their parent's; the trash counts their notes too.
    const count = nb.id === "trash" ? state.trashed.reduce((a, n) => a + n.count, nb.count) : withSubs(state.notebooks, nb);
    const li = item(nb.id, nb.name, nb.kind, count, nb.id === "trash" || nb.id === "inbox" ? "sep" : "");
    if (!nb.reserved) enableNotebookDrag(li, nb.id);
    if (nb.id === "trash") acceptNotebookTrash(li);
    ul.append(li);
  }
}

// renderTags appends the tags to ul. The selected tag stays even when no note
// has it any more, so that the list does not change under the user.
function renderTags(ul) {
  const tags = [...state.tags];
  if (state.tag && !tags.some((tg) => tg.name === state.tag)) tags.push({ name: state.tag, count: 0 });
  if (!tags.length) {
    ul.append(el("li", "empty sep", t("noTags")));
    return;
  }
  tags.forEach((tg, i) => {
    const li = el("li", (i === 0 ? "sep" : "") + (state.tag === tg.name ? " active" : ""),
      el("span", "icon", "#"), el("span", "name", tg.name), el("span", "count", String(tg.count)));
    li.addEventListener("click", () => selectTag(tg.name));
    ul.append(li);
  });
}

// Notebook order: the notebooks between inbox and trash, which stay fixed, can
// be dragged. As in the TUI, their orders are renumbered 10 apart and only
// the changed ones are saved, one at a time.
let nbDragID = null;
let notebookSaves = Promise.resolve();

// canHoldSub reports whether notebook nb can be moved into notebook target,
// as a sub notebook: one level deep, so not a notebook with sub notebooks.
function canHoldSub(target, nb) {
  return !!target && !!nb && !target.reserved && !target.parent && !nb.reserved && target.id !== nb.id &&
    nb.parent !== target.id && !state.notebooks.some((n) => n.parent === nb.id);
}

// notebookDrop tells where the notebook dragged goes when dropped on li: into
// it, as a sub notebook, when dropped on the middle of the row; else before a
// notebook (null for last), and whether below li. A sub notebook only goes
// into a notebook.
function notebookDrop(li, e) {
  const target = state.notebooks.find((n) => n.id === li.dataset.id);
  const dragged = state.notebooks.find((n) => n.id === nbDragID);
  const r = li.getBoundingClientRect();
  const y = (e.clientY - r.top) / r.height;
  const into = canHoldSub(target, dragged) ? target.id : null;
  if (dragged?.parent || (into && y > 0.25 && y < 0.75)) return { into };
  const below = y > 0.5;
  const others = state.notebooks.filter((n) => !n.reserved && !n.parent && n.id !== nbDragID);
  const i = others.findIndex((n) => n.id === li.dataset.id);
  return { before: below ? (others[i + 1]?.id ?? null) : others[i].id, below };
}

function enableNotebookDrag(li, id) {
  li.dataset.id = id;
  li.draggable = true;
  li.title = t("dragToReorder");
  li.addEventListener("dragstart", (e) => {
    nbDragID = id;
    e.dataTransfer.effectAllowed = "move";
    li.classList.add("dragging");
  });
  li.addEventListener("dragend", endNotebookDrag);
  li.addEventListener("dragover", (e) => {
    if (!nbDragID || nbDragID === id) return;
    const { into, before, below } = notebookDrop(li, e);
    if (into === null) return; // a sub notebook that cannot go there
    e.preventDefault();
    li.classList.toggle("drop-into", !!into);
    li.classList.toggle("drop-below", !into && below);
    li.classList.toggle("drop-above", !into && !below && before !== undefined);
  });
  li.addEventListener("dragleave", () => li.classList.remove("drop-above", "drop-below", "drop-into"));
  li.addEventListener("drop", (e) => {
    if (!nbDragID || nbDragID === id) return;
    const moved = nbDragID;
    const { into, before } = notebookDrop(li, e);
    if (into === null) return;
    e.preventDefault();
    li.classList.remove("drop-into");
    if (into) moveNotebookInto(state.notebooks.find((n) => n.id === moved), into);
    else moveNotebook(moved, before);
  });
}

// endNotebookDrag clears what a notebook being dragged left on the rows.
function endNotebookDrag() {
  nbDragID = null;
  for (const x of [...$("notebooks").children, ...$("notes").children]) {
    x.classList.remove("dragging", "drop-above", "drop-below", "drop-into");
  }
}

// moveNotebookInto moves notebook nb into the notebook parent, as a sub
// notebook, or to the top with a null parent, and offers to undo it. Moving a
// private notebook where AI clients see it is asked first, as changing its
// kind is.
async function moveNotebookInto(nb, parent, undoing) {
  if (!nb) return;
  // A notebook moved into another keeps its kind as its own; a sub notebook
  // without one takes that of its new parent.
  const kind = parent ? subKind(nb.parent ? nb.own_kind : nb.kind, parent) : nb.kind;
  if (!undoing && nb.kind === "private" && kind !== "private" && !confirm(t("confirmUnprivate", { name: nb.name }))) return;
  const shown = state.notebook === nb.id;
  let moved;
  try {
    moved = await api(`/notebooks/move/${nbPath(nb.id)}`, { method: "POST", body: parent ? { parent } : {} });
  } catch (e) {
    if (undoing) throw e;
    notebookFailed(e);
    return;
  }
  if (!undoing) {
    // Back where it was, with the kind it had: one moved back to the top
    // would keep the kind of the parent.
    const back = nb.parent || null;
    offerUndo(t("toastNotebookMoved", { name: nb.name, where: parent ? notebookName(parent) : t("notebookTop") }), async () => {
      await moveNotebookInto({ ...moved, kind }, back, true);
      if (!back && kind !== nb.kind) await api(`/notebooks/${nbPath(nb.id)}`, { method: "PUT", body: { kind: nb.kind } });
      await loadNotebooks().catch(() => {});
    });
  }
  await loadNotebooks().catch(() => {});
  if (shown) showNotebook(moved.id);
  else loadNotes();
}

// acceptNotebookTrash lets a notebook dragged in the column be dropped on the
// trash row li, which moves it to the trash after asking, as its menu does.
function acceptNotebookTrash(li) {
  li.addEventListener("dragover", (e) => {
    if (!nbDragID) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = "move";
    li.classList.add("drop-into");
  });
  li.addEventListener("dragleave", () => li.classList.remove("drop-into"));
  li.addEventListener("drop", (e) => {
    if (!nbDragID) return;
    e.preventDefault();
    li.classList.remove("drop-into");
    const nb = state.notebooks.find((n) => n.id === nbDragID);
    if (nb) trashNotebook(nb);
  });
}

async function moveNotebook(id, before) {
  const moved = state.notebooks.find((n) => n.id === id);
  const others = state.notebooks.filter((n) => !n.reserved && !n.parent && n.id !== id);
  others.splice(before ? others.findIndex((n) => n.id === before) : others.length, 0, moved);
  const changed = [];
  others.forEach((nb, k) => {
    const order = (k + 1) * 10;
    if (nb.order !== order) {
      nb.order = order;
      changed.push({ id: nb.id, order });
    }
  });
  // Show the new order at once; inbox stays first and trash last, and the
  // sub notebooks follow their parents.
  state.notebooks = [
    ...state.notebooks.filter((n) => n.reserved && n.id !== "trash"),
    ...others.flatMap((nb) => [nb, ...state.notebooks.filter((n) => n.parent === nb.id)]),
    ...state.notebooks.filter((n) => n.id === "trash"),
  ];
  renderNotebooks();
  notebookSaves = notebookSaves.then(async () => {
    try {
      for (const c of changed) {
        await api(`/notebooks/${nbPath(c.id)}`, { method: "PUT", body: { order: c.order } });
      }
    } catch (e) {
      alert(t("notebookOrderNotSaved", { error: e.message }));
    }
  });
  await notebookSaves;
  loadNotebooks().catch(() => {});
}

// selectNotebook lists the notes of notebook id; with id "trash" and
// trashed, those of the notebook trashed, which is in the trash.
function selectNotebook(id, trashed = null) {
  if (!state.tag && id === state.notebook && trashed === state.trashedNotebook) return;
  state.tag = null;
  state.notebook = id;
  state.trashedNotebook = id === "trash" ? trashed : null;
  renderNotebooks();
  loadNotes({ chosen: true, first: true });
}

// showNotebook shows notebook id after it was created, moved or put back: a
// sub notebook opens in the note list of its parent, as its cell would.
function showNotebook(id) {
  const parent = parentOf(id);
  if (!parent) {
    selectNotebook(id);
    return;
  }
  state.expanded.add(subKey(id, false));
  saveExpanded();
  if (state.tag || state.notebook !== parent || state.trashedNotebook) selectNotebook(parent);
  else loadNotes();
}

// Notebook actions: the menu of a notebook row, opened with its "…" button or
// a right click. The kinds follow the rules of the TUI: inbox cannot be
// collab, and the trash has no kind.

const KINDS = ["default", "collab", "drop", "private"];
const KIND_LABEL = { default: "kindDefault", collab: "kindCollab", drop: "kindDrop", private: "kindPrivate" };

let menuRow = null; // the row whose menu is open

function openNotebookMenu(li, id, trashed, e) {
  const m = $("notebook-menu");
  m.replaceChildren();
  const item = (label, fn, cls) => {
    const b = button(label, () => {
      closeNotebookMenu();
      fn();
    }, { cls });
    b.setAttribute("role", "menuitem");
    m.append(b);
  };
  const sep = () => m.append(Object.assign(el("div", "menu-sep"), { role: "separator" }));
  // A sub notebook, opened under its parent, can be listed alone too.
  const sub = trashed ? state.trashed.find((x) => x.id === trashed) : state.notebooks.find((x) => x.id === id);
  if ((sub?.parent || trashed) && (trashed ? state.trashedNotebook !== trashed : state.notebook !== id)) {
    item(t("subNotebookOpenAlone"), () => selectNotebook(id, trashed));
    sep();
  }
  if (trashed) {
    item(t("notebookRestore"), () => restoreNotebook(trashed));
    item(t("notebookDeletePermanently"), () => purgeNotebook(trashed), "danger");
  } else {
    const nb = state.notebooks.find((n) => n.id === id);
    if (!nb) return;
    item(t("notebookRename"), () => renameNotebook(id));
    if (!nb.reserved && !nb.parent) item(t("subNotebookNew"), () => openNewNotebook(id));
    if (nb.parent) item(t("notebookMoveTop"), () => moveNotebookInto(nb, null));
    const parents = state.notebooks.filter((p) => canHoldSub(p, nb));
    if (parents.length) {
      sep();
      m.append(el("div", "menu-label", t("notebookMoveInto")));
      for (const p of parents) item(p.name, () => moveNotebookInto(nb, p.id));
    }
    if (id !== "trash") {
      // A sub notebook takes the kind of its parent unless it sets its own.
      const cur = nb.parent ? nb.own_kind || "inherit" : nb.kind;
      const parentKind = nb.parent && state.notebooks.find((n) => n.id === nb.parent)?.kind;
      sep();
      m.append(el("div", "menu-label", t("notebookKind")));
      for (const k of nb.parent ? ["inherit", ...KINDS] : KINDS) {
        if (id === "inbox" && k === "collab") continue;
        const inherit = k === "inherit";
        const b = button(inherit ? t("kindInherit", { kind: parentKind || "default", name: notebookName(nb.parent) }) : k, () => {
          closeNotebookMenu();
          if (k !== cur) setNotebookKind(nb, k);
        });
        const svg = kindSVG(inherit ? parentKind : k);
        if (svg) b.append(el("span", "kind-icon", svg));
        if (!inherit) b.title = t(KIND_LABEL[k]);
        b.setAttribute("role", "menuitemradio");
        b.setAttribute("aria-checked", String(k === cur));
        m.append(b);
      }
      if (parentKind === "private") m.append(el("div", "menu-label", t("kindInPrivate", { name: notebookName(nb.parent) })));
    }
    if (!nb.reserved) {
      sep();
      item(t("notebookToTrash"), () => trashNotebook(nb), "danger");
    }
    if (id === "trash" && (nb.count || state.trashed.length)) {
      sep();
      item(t("emptyTrash"), emptyTrash, "danger");
    }
  }
  closeNotebookMenu();
  menuRow = li;
  li.classList.add("menu-open");
  m.hidden = false;
  // At the pointer for a right click, else beside the row; kept on screen.
  const r = li.getBoundingClientRect();
  const x = e ? e.clientX : r.right - 8;
  const y = e ? e.clientY : r.bottom;
  m.style.left = Math.min(x, innerWidth - m.offsetWidth - 8) + "px";
  m.style.top = Math.min(y, innerHeight - m.offsetHeight - 8) + "px";
  m.querySelector("button")?.focus();
}

function closeNotebookMenu() {
  $("notebook-menu").hidden = true;
  menuRow?.classList.remove("menu-open");
  menuRow = null;
}

document.addEventListener("click", (e) => {
  if (!$("notebook-menu").hidden && !e.target.closest("#notebook-menu")) closeNotebookMenu();
});
$("notebooks").addEventListener("scroll", closeNotebookMenu);
window.addEventListener("resize", closeNotebookMenu);
$("notebook-menu").addEventListener("keydown", (e) => {
  if (e.key === "Escape") {
    e.preventDefault();
    e.stopPropagation();
    const row = menuRow;
    closeNotebookMenu();
    row?.querySelector(".nb-more")?.focus();
  }
});

// notebookFailed shows why an operation on a notebook failed and reloads the
// notebooks, which may have changed meanwhile.
function notebookFailed(e) {
  alert(t("notebookFailed", { error: e.message }));
  loadNotebooks().catch(() => {});
}

// renameNotebook turns the name of the notebook row into a field: Enter or
// leaving it saves, Esc drops the change. The folder (the ID) keeps its name.
let renaming = null;

function renameNotebook(id) {
  const nb = state.notebooks.find((n) => n.id === id);
  const li = [...$("notebooks").children, ...$("notes").children].find((x) => x.dataset.notebook === id && !x.classList.contains("trashed"));
  if (!nb || !li) return;
  renaming = id;
  li.draggable = false;
  const input = el("input", "nb-rename");
  input.value = nb.name;
  input.maxLength = 100;
  input.title = t("notebookRenameHint", { id });
  input.setAttribute("aria-label", t("notebookName"));
  li.querySelector(".name").replaceWith(input);
  input.focus();
  input.select();
  let done = false;
  const finish = async (save) => {
    if (done) return;
    done = true;
    renaming = null;
    const name = input.value.trim();
    if (save && name && name !== nb.name) {
      try {
        await api(`/notebooks/${nbPath(id)}`, { method: "PUT", body: { name } });
      } catch (e) {
        notebookFailed(e);
        return;
      }
      const old = nb.name;
      offerUndo(t("toastRenamed", { name }), async () => {
        await api(`/notebooks/${nbPath(id)}`, { method: "PUT", body: { name: old } });
        await loadNotebooks().catch(() => {});
        loadNotes();
      });
      await loadNotebooks().catch(() => {});
      loadNotes();
      return;
    }
    renderNotebooks();
  };
  input.addEventListener("click", (e) => e.stopPropagation());
  input.addEventListener("keydown", (e) => {
    e.stopPropagation();
    if (e.isComposing || e.keyCode === 229) return; // Safari ends an IME conversion with keyCode 229
    if (e.key === "Enter") {
      e.preventDefault();
      finish(true);
    } else if (e.key === "Escape") {
      e.preventDefault();
      finish(false);
    }
  });
  input.addEventListener("blur", () => finish(true));
}

async function setNotebookKind(nb, kind, undoing) {
  const after = nb.parent ? subKind(kind === "inherit" ? undefined : kind, nb.parent) : kind;
  if (nb.kind === "private" && (after === "default" || after === "collab") && !confirm(t("confirmUnprivate", { name: nb.name }))) return;
  try {
    await api(`/notebooks/${nbPath(nb.id)}`, { method: "PUT", body: { kind } });
  } catch (e) {
    if (undoing) throw e;
    notebookFailed(e);
    return;
  }
  if (!undoing) {
    const old = nb.parent ? nb.own_kind || "inherit" : nb.kind;
    const now = { ...nb, kind: after, own_kind: kind === "inherit" ? undefined : kind };
    offerUndo(t("toastKind", { name: nb.name, kind: after }), () => setNotebookKind(now, old, true));
  }
  await loadNotebooks().catch(() => {});
}

// subKind returns the kind in effect of a sub notebook of parent with its own
// kind own (undefined for none): private in a private parent, else its own,
// else that of the parent, as the server decides.
function subKind(own, parent) {
  const p = state.notebooks.find((n) => n.id === parent)?.kind || "default";
  return p === "private" ? "private" : own || p;
}

// trashNotebook moves a notebook with its notes to the trash, where it is
// listed under the trash and can be restored, and shows it there.
async function trashNotebook(nb) {
  // The notes of its sub notebooks go with it.
  const n = nb.count + state.notebooks.filter((x) => x.parent === nb.id).reduce((a, x) => a + x.count, 0);
  if (!confirm(t("confirmTrashNotebook", { name: notebookName(nb.id), n }))) return;
  try {
    await api(`/notebooks/${nbPath(nb.id)}`, { method: "DELETE" });
  } catch (e) {
    notebookFailed(e);
    return;
  }
  const id = trashedID(nb);
  offerUndo(t("toastNotebookTrashed", { name: nb.name }), async () => {
    await api(`/trash/notebooks/restore/${nbPath(id)}`, { method: "POST" });
    await loadNotebooks().catch(() => {});
    showNotebook(nb.id);
  });
  const shown = state.notebook === nb.id || parentOf(state.notebook) === nb.id;
  const inParent = state.notebook !== nb.id && shown ? state.notebook.slice(nb.id.length + 1) : null;
  await loadNotebooks().catch(() => {});
  if (shown) selectNotebook("trash", inParent ? `${id}/${inParent}` : id);
}

// emptyTrash permanently deletes everything in the trash, the notes deleted
// on their own and the notebooks with their notes, after asking, as it cannot
// be undone.
async function emptyTrash() {
  const single = state.notebooks.find((n) => n.id === "trash")?.count ?? 0;
  const notes = single + state.trashed.reduce((a, n) => a + n.count, 0);
  if (!confirm(t("confirmEmptyTrash", { n: notes, m: state.trashed.length }))) return;
  try {
    await api("/trash", { method: "DELETE" });
  } catch (e) {
    notebookFailed(e);
    return;
  }
  await loadNotebooks().catch(() => {});
  if (state.notebook === "trash") await loadNotes();
}

async function restoreNotebook(id) {
  let nb;
  try {
    nb = await api(`/trash/notebooks/restore/${nbPath(id)}`, { method: "POST" });
  } catch (e) {
    notebookFailed(e);
    return;
  }
  await loadNotebooks().catch(() => {});
  showNotebook(nb.id);
}

async function purgeNotebook(id) {
  const tn = state.trashed.find((n) => n.id === id);
  if (!confirm(t("confirmPurgeNotebook", { name: tn?.name ?? id, n: tn?.count ?? 0 }))) return;
  try {
    await api(`/trash/notebooks/${nbPath(id)}`, { method: "DELETE" });
  } catch (e) {
    notebookFailed(e);
    return;
  }
  if (state.trashedNotebook === id) {
    state.trashedNotebook = null;
    state.selected = null;
  }
  await loadNotebooks().catch(() => {});
  loadNotes();
  loadNote();
}

// The dialog of a new notebook: its name and kind. The ID, the name of its
// folder, is made from the name by the server.
// With parent, the new notebook is a sub notebook of it, which takes its kind
// until its menu sets one.
function openNewNotebook(parent = null) {
  const d = $("notebook-new");
  $("notebook-new-form").reset();
  $("notebook-new-error").hidden = true;
  d.dataset.parent = parent || "";
  d.querySelector("h2").textContent = parent ? t("subNotebookNewIn", { name: notebookName(parent) }) : t("notebookNew");
  d.querySelector("fieldset.kinds").hidden = !!parent;
  const inherited = $("notebook-new-inherited");
  inherited.hidden = !parent;
  if (parent) {
    const p = state.notebooks.find((n) => n.id === parent);
    inherited.textContent = t("kindInherited", { kind: p?.kind ?? "default", name: notebookName(parent) });
  }
  d.showModal();
  $("notebook-new-name").focus();
}

$("notebook-add").addEventListener("click", () => openNewNotebook());
// Cancel is not a submit button: Enter in the name submits with the first
// submit button of the form, which must be the one that creates.
$("notebook-new-cancel").addEventListener("click", () => $("notebook-new").close());
$("notebook-new-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  const name = $("notebook-new-name").value.trim();
  if (!name) return;
  const kind = new FormData($("notebook-new-form")).get("kind");
  const parent = $("notebook-new").dataset.parent || undefined;
  let nb;
  try {
    nb = await api("/notebooks", { method: "POST", body: { name, kind, parent } });
  } catch (err) {
    const msg = $("notebook-new-error");
    msg.textContent = err.status === 409 ? t("notebookNameTaken", { name }) : t("notebookFailed", { error: err.message });
    msg.hidden = false;
    return;
  }
  $("notebook-new").close();
  if (state.side !== "notebooks") setSide("notebooks");
  await loadNotebooks().catch(() => {});
  showNotebook(nb.id);
});

// selectTag lists the notes with tag in all notebooks but the trash, as
// choosing a notebook does.
function selectTag(tag) {
  if (state.side !== "tags") setSide("tags"); // show the tag chosen in the preview
  if (tag === state.tag) return;
  state.tag = tag;
  state.notebook = null;
  state.trashedNotebook = null;
  renderNotebooks();
  loadNotes({ chosen: true, first: true });
}

// Notes

let loadSeq = 0; // drops responses of requests overtaken by later ones

// checkSelection is set when the user changes the list, by choosing a notebook
// or a search query. The note in the preview then stays only if the new list
// has it. Reloads on change events and after saving do not clear it, so that a
// note just moved elsewhere stays in view.
let checkSelection = false;

// pickFirst is set when the user chooses a notebook: the preview then shows
// the first note of the new list, as in the TUI.
let pickFirst = false;

async function loadNotes({ chosen = false, first = false } = {}) {
  if (chosen) {
    state.chosen.clear();
    checkSelection = true;
    pickFirst = first;
    state.want = LIST_PAGE; // a new list starts with one page
  }
  state.want ||= LIST_PAGE;
  const seq = ++loadSeq;
  const info = $("list-info");
  try {
    let notes, total;
    if (state.query) {
      const r = await api("/search", { params: { q: state.query, notebook: state.notebook, tag: state.tag, limit: 200 } });
      notes = r.notes;
      total = r.total;
      if (state.trashedNotebook) {
        // The search takes no notebook in the trash: the notes of the trash
        // are narrowed here.
        notes = notes.filter((n) => n.trashed_notebook === state.trashedNotebook);
        total = notes.length;
      }
    } else {
      // A notebook's list comes in its own sort, which the server knows.
      // Pages are read until the notes shown so far, so that a reload keeps
      // the pages read with "load more".
      notes = [];
      do {
        const r = await api("/notes", { params: { notebook: state.notebook, trashed_notebook: state.trashedNotebook, tag: state.tag, sort: state.notebook ? null : state.allSort, limit: LIST_PAGE, offset: notes.length } });
        notes.push(...r.notes);
        total = r.total;
        if (!r.notes.length) break;
      } while (notes.length < Math.min(state.want, total));
    }
    const subs = new Map();
    await Promise.all(openSubs().map(async (x) => {
      try {
        subs.set(x.key, await readSubNotes(x));
      } catch (_) {
        subs.set(x.key, { notes: [], total: 0, want: LIST_PAGE }); // shown empty; the row stays
      }
    }));
    if (seq !== loadSeq) return;
    state.notes = notes;
    state.total = total;
    state.subNotes = subs;
    const many = state.chosen.size > 1;
    const listed = listedNotes();
    for (const id of state.chosen) {
      if (!listed.some((n) => n.id === id)) state.chosen.delete(id);
    }
    if (state.chosen.size < 2) state.chosen.clear();
    if (many && !state.chosen.size) loadNote(); // back to the preview
    const where = state.tag ? "#" + state.tag
      : state.trashedNotebook ? notebookName("trash") + " / " + trashedName(state.trashedNotebook)
      : state.notebook ? notebookName(state.notebook) : t("all");
    info.textContent = state.query
      ? t(total > notes.length ? "searchInfoTop" : "searchInfo", { where, query: state.query, total, shown: notes.length })
      : t(total > notes.length ? "listInfoPart" : "listInfo", { where, total, shown: notes.length });
    info.classList.remove("error");
    renderSort();
    renderNotes();
    applySelectionCheck();
    saveView();
  } catch (e) {
    if (seq !== loadSeq) return;
    state.notes = [];
    info.textContent = t("loadFailed", { error: e.message });
    info.classList.add("error");
    renderNotes();
  }
}

// applySelectionCheck clears the preview if the user changed the list and the
// note is not in it. The editor stays open with its unsaved changes; the check
// waits until it closes.
function applySelectionCheck() {
  if (!checkSelection || state.editing) return;
  checkSelection = false;
  if (pickFirst) {
    pickFirst = false;
    const first = listedNotes()[0]?.id ?? null;
    if (first !== state.selected) {
      state.selected = first;
      markSelected();
      loadNote();
      return;
    }
  }
  if (state.selected && !listedNotes().some((n) => n.id === state.selected)) {
    state.selected = null;
    loadNote();
  } else {
    markSelected();
  }
}

function renderSort() {
  const sel = $("sort");
  sel.hidden = !!state.query; // search results come by relevance
  sel.value = currentSort();
  sel.title = t(canReorder() ? "sortTitleReorder" : "sortTitle");
}

// Sort changes are saved one at a time, so that quick key presses reach the
// server in order and the last one wins.
let sortSaves = Promise.resolve();

async function setSort(sort) {
  if (!state.notebook) {
    state.allSort = sort;
    try { localStorage.setItem("ebira.allSort", sort); } catch (_) { /* not kept */ }
    loadNotes();
    return;
  }
  // Take the new sort at once, so that the next key press cycles from it.
  const id = state.notebook;
  const cur = state.notebooks.find((n) => n.id === id);
  if (cur) cur.sort = sort;
  renderSort();
  sortSaves = sortSaves.then(async () => {
    try {
      await api(`/notebooks/${nbPath(id)}`, { method: "PUT", body: { sort } });
    } catch (e) {
      alert(t("sortNotSaved", { error: e.message }));
    }
  });
  await sortSaves;
  loadNotes();
}

$("sort").addEventListener("change", (e) => setSort(e.target.value));

function cycleSort() {
  const sorts = ["order", "created", "updated", "title"];
  setSort(sorts[(sorts.indexOf(currentSort()) + 1) % sorts.length]);
}

// moveNote puts note id right after the note after (null for first) in the
// manual order.
async function moveNote(id, after, undoing) {
  // The note above it now, to put it back after.
  const { notes } = listOf(id);
  const i = notes.findIndex((n) => n.id === id);
  const before = i > 0 ? notes[i - 1].id : null;
  try {
    await api(`${noteURL(id)}/move`, { method: "POST", body: after ? { after } : {} });
  } catch (e) {
    if (undoing) throw e;
    alert(t("moveFailed", { error: e.message }));
    await loadNotes();
    return;
  }
  if (!undoing && i >= 0) {
    offerUndo(t("toastReordered", { title: notes[i].title || t("untitled") }), () => moveNote(id, before, true));
  }
  await loadNotes();
  markSelected();
}

// moveSelected moves the selected note up (-1) or down (+1).
function moveSelected(delta) {
  if (state.query || !state.selected) return;
  const { notes, reorder } = listOf(state.selected);
  if (!reorder) return;
  const i = notes.findIndex((n) => n.id === state.selected);
  const j = i + delta;
  if (i < 0 || j < 0 || j >= notes.length) return;
  // Down: after the next note. Up: after the note above the previous one, or
  // first.
  const after = delta > 0 ? notes[j].id : (notes[j - 1]?.id ?? null);
  moveNote(state.selected, after);
}

// Drag and drop in the manual order: dropping on the upper half of a note puts
// the dragged note above it, on the lower half below it.
let dragID = null;

function dropAfter(li, e, list) {
  const r = li.getBoundingClientRect();
  const below = e.clientY > r.top + r.height / 2;
  const others = list.filter((n) => n.id !== dragID);
  const i = others.findIndex((n) => n.id === li.dataset.id);
  if (below) return { after: others[i].id, below };
  return { after: i > 0 ? others[i - 1].id : null, below };
}

// enableDrag lets the note li be dragged onto a notebook, to move it there,
// and, with reorder, within list, the notes it is shown with: those of the
// notebook, or of an opened sub notebook.
function enableDrag(li, reorder, list) {
  li.draggable = true;
  li.addEventListener("dragstart", (e) => {
    dragID = li.dataset.id;
    e.dataTransfer.effectAllowed = "move";
    e.dataTransfer.setData("text/plain", draggedNotes()?.length ? t("selectionCount", { n: draggedNotes().length }) : li.querySelector(".title")?.textContent || "");
    li.classList.add("dragging");
  });
  li.addEventListener("dragend", () => {
    dragID = null;
    li.classList.remove("dragging");
    for (const x of $("notes").children) x.classList.remove("drop-above", "drop-below", "drop-into");
    for (const x of $("notebooks").children) x.classList.remove("drop-into");
  });
  if (!reorder) return;
  const within = () => dragID && dragID !== li.dataset.id && !draggedNotes() && list.some((n) => n.id === dragID);
  li.addEventListener("dragover", (e) => {
    if (!within()) return;
    e.preventDefault();
    const { below } = dropAfter(li, e, list);
    li.classList.toggle("drop-below", below);
    li.classList.toggle("drop-above", !below);
  });
  li.addEventListener("dragleave", () => li.classList.remove("drop-above", "drop-below"));
  li.addEventListener("drop", (e) => {
    if (!within()) return;
    e.preventDefault();
    const id = dragID;
    const { after } = dropAfter(li, e, list);
    moveNote(id, after);
  });
}

// draggedNotes returns the IDs of the chosen notes when one of them is being
// dragged, which then go together; else null.
function draggedNotes() {
  return dragID && state.chosen.size > 1 && state.chosen.has(dragID) ? [...state.chosen] : null;
}

// subNotebookRows returns the rows the note list starts with: the sub
// notebooks of the notebook shown, or the way back up from a sub notebook.
// They show with a notebook (in the trash too), not with a search or a tag.
function subNotebookRows() {
  if (state.query || state.tag || !state.notebook) return [];
  const row = (cls, icon, name, count, open) => {
    const li = el("li", "sub-notebook " + cls, el("span", "icon", kindSVG(icon)), el("span", "name", name));
    if (count !== null) li.append(el("span", "count", String(count)));
    li.addEventListener("click", open);
    return li;
  };
  const trashed = state.notebook === "trash";
  const cur = trashed ? state.trashedNotebook : state.notebook;
  if (!cur && !trashed) return [];
  const up = (to, name) => {
    const li = row("up", "up", t("subNotebookUp", { name }), null, () => selectNotebook(trashed ? "trash" : to, trashed ? to : null));
    li.title = t("subNotebookUp", { name });
    return li;
  };
  const parent = cur && parentOf(cur, trashed);
  if (parent) return [up(parent, trashed ? trashedName(parent) : notebookName(parent))];
  // A notebook in the trash, listed alone, goes back up to the trash.
  const head = trashed && cur ? [up(null, notebookName("trash"))] : [];
  return [...head, ...childNotebooks(cur, trashed).flatMap((sub) => cellRows(sub, 0))];

  // cellRows returns the row of notebook sub and, when it is open, the rows
  // under it: in the trash itself, the rows of its sub notebooks in the trash
  // (depth 1), then its notes.
  function cellRows(sub, depth) {
    const key = subKey(sub.id, trashed);
    const open = state.expanded.has(key);
    const li = row((open ? "open" : "") + (depth ? " nested" : ""), "folder", sub.name, withSubs(trashed ? state.trashed : state.notebooks, sub), () => toggleSub(key));
    li.prepend(el("span", "twisty", kindSVG("chevron")));
    // The folder holds the mark of the kind in effect.
    if (KIND_SVG[sub.kind]) {
      const icon = li.querySelector(".icon");
      icon.replaceChildren(folderSVG(sub.kind));
      icon.title = sub.kind + ": " + t("kind" + sub.kind[0].toUpperCase() + sub.kind.slice(1));
    }
    li.setAttribute("aria-expanded", String(open));
    li.dataset.notebook = trashed ? "trash" : sub.id;
    li.title = t(open ? "subNotebookCollapse" : "subNotebookExpand", { name: sub.name });
    li.addEventListener("contextmenu", (e) => {
      e.preventDefault();
      openNotebookMenu(li, trashed ? "trash" : sub.id, trashed ? sub.id : null, e);
    });
    const more = button("…", (e) => {
      e.stopPropagation();
      openNotebookMenu(li, trashed ? "trash" : sub.id, trashed ? sub.id : null);
    }, { cls: "nb-more", title: t("notebookActions") });
    more.setAttribute("aria-haspopup", "menu");
    more.setAttribute("aria-label", t("notebookActions"));
    li.append(more);
    if (!trashed) {
      acceptImport(li, sub.id);
      acceptNote(li, sub.id);
      // Dragged onto a notebook of the left column, it goes into it; onto
      // the trash, to the trash.
      li.draggable = true;
      li.addEventListener("dragstart", (e) => {
        nbDragID = sub.id;
        e.dataTransfer.effectAllowed = "move";
        li.classList.add("dragging");
      });
      li.addEventListener("dragend", endNotebookDrag);
    }
    if (!open) return [li];
    const nested = trashed && !cur && depth === 0 ? childNotebooks(sub.id, true).flatMap((c) => cellRows(c, 1)) : [];
    // Its notes, under it.
    const s = state.subNotes.get(key) ?? { notes: [], total: 0 };
    const reorder = !trashed && (sub.sort || "order") === "order";
    const cls = depth ? "in-sub deep" : "in-sub";
    const rows = s.notes.map((n) => noteRow(n, sub.sort || "order", reorder, s.notes, cls));
    if (s.notes.length < s.total) {
      const more = button(t("loadMore", { rest: s.total - s.notes.length }), () => {
        s.want = s.notes.length + LIST_PAGE;
        loadNotes();
      }, { cls: "more" });
      rows.push(el("li", "more-row " + cls, more));
    }
    return [li, ...nested, ...rows];
  }
}

function renderNotes() {
  if (renaming && parentOf(renaming)) return; // a sub notebook row is being renamed
  const ul = $("notes");
  ul.replaceChildren(...subNotebookRows());
  for (const n of state.notes) ul.append(noteRow(n, currentSort(), canReorder(), state.notes, ""));
  if (!state.query && state.notes.length < state.total) {
    const more = button(t("loadMore", { rest: state.total - state.notes.length }), () => {
      state.want = state.notes.length + LIST_PAGE;
      loadNotes();
    }, { cls: "more" });
    ul.append(el("li", "more-row", more));
  }
}

// noteRow returns the row of note n in the list, shown in sort; with reorder,
// it can be dragged within list, the notes it is shown with.
function noteRow(n, sort, reorder, list, cls) {
  const li = el("li", [cls, n.id === state.selected ? "active" : "", state.chosen.has(n.id) ? "chosen" : ""].filter(Boolean).join(" "));
  li.dataset.id = n.id;
  enableDrag(li, reorder && n.notebook !== "trash", list); // a note in the trash can be moved to a notebook
  const head = el("div", "title-row", ...noteMarks(n), el("div", "title", highlighted(n.title || t("untitled"), n.title_highlights)));
  const mark = aiMark(n);
  if (mark) head.append(mark);
  li.append(head);
  li.addEventListener("contextmenu", (e) => {
    if (n.notebook === "trash") return;
    e.preventDefault();
    openMarkMenu(n, li, e);
  });
  // The time shown follows the sort: creation for the creation order (and
  // the manual order, which starts as it), update otherwise.
  const created = !state.query && (sort === "created" || sort === "order");
  const time = created ? t("createdAt", { time: formatTime(n.created_at) }) : formatTime(n.updated_at);
  const meta = el("div", "meta", el("span", "", time));
  // Searches in a notebook find the notes of its sub notebooks too.
  if (!state.notebook || n.notebook !== state.notebook) meta.append(el("span", "", notebookName(n.notebook)));
  meta.append(...tagList(n.tags));
  li.append(meta);
  if (n.snippet) li.append(el("div", "snippet", highlighted(n.snippet, n.snippet_highlights)));
  li.addEventListener("click", (e) => chooseNote(n.id, e));
  li.addEventListener("mousedown", (e) => {
    if (e.shiftKey) e.preventDefault(); // choosing, not selecting text
  });
  return li;
}

function markSelected() {
  for (const li of $("notes").children) li.classList.toggle("active", li.dataset.id === state.selected);
  $("notes").querySelector("li.active")?.scrollIntoView({ block: "nearest" });
}

function selectNote(id) {
  if (state.chosen.size) {
    state.chosen.clear();
    markChosen();
    if (id === state.selected && !state.editing) {
      loadNote(); // back from the chosen notes
      return;
    }
  }
  if (id === state.selected && !state.editing) return;
  if (!leaveEditor()) return;
  state.selected = id;
  markSelected();
  loadNote();
}

// chooseNote handles a click on note id in the list: with Cmd or Ctrl, the
// note joins the chosen notes or leaves them; with Shift, the notes from the
// last one clicked are chosen; else the note is shown alone. Two or more
// chosen notes replace the preview with what can be done to them.
function chooseNote(id, e) {
  if (!(e.metaKey || e.ctrlKey || e.shiftKey)) {
    state.anchor = id;
    selectNote(id);
    return;
  }
  if (!leaveEditor()) return;
  const ids = listedNotes().map((n) => n.id);
  if (e.shiftKey && ids.includes(state.anchor ?? state.selected)) {
    const a = ids.indexOf(state.anchor ?? state.selected), b = ids.indexOf(id);
    state.chosen = new Set(ids.slice(Math.min(a, b), Math.max(a, b) + 1));
  } else {
    if (!state.chosen.size && state.selected && ids.includes(state.selected)) state.chosen.add(state.selected);
    if (state.chosen.has(id)) state.chosen.delete(id);
    else state.chosen.add(id);
    state.anchor = id;
  }
  if (state.chosen.size < 2) {
    const only = [...state.chosen][0] ?? state.selected;
    state.chosen.clear();
    markChosen();
    if (only) {
      state.selected = null; // shown again
      selectNote(only);
    }
    return;
  }
  markChosen();
  showChosen();
}

// chooseAll chooses all the notes in the list.
function chooseAll() {
  const notes = listedNotes();
  if (notes.length < 2 || !leaveEditor()) return;
  state.chosen = new Set(notes.map((n) => n.id));
  markChosen();
  showChosen();
}

function clearChosen() {
  state.chosen.clear();
  markChosen();
  loadNote();
}

function markChosen() {
  for (const li of $("notes").children) li.classList.toggle("chosen", !!li.dataset.id && state.chosen.has(li.dataset.id));
}

// showChosen shows the chosen notes in place of the preview, with what can be
// done to them: move them to a notebook, move them to the trash.
function showChosen() {
  const ids = [...state.chosen];
  const notes = ids.map(findListed).filter(Boolean);
  noteSeq++; // drop a preview being loaded
  state.note = null;
  const actions = el("div", "actions", moveSelect((nb) => moveNotesTo(ids, nb)));
  if (!notes.some((n) => n.notebook === "trash")) actions.append(button(t("toTrash"), () => moveNotesTo(ids, "trash")));
  actions.append(button(t("selectionClear"), clearChosen, { title: t("selectionClearTitle") }));
  const list = el("ul", "chosen-list", ...notes.map((n) => el("li", "", el("span", "", n.title || t("untitled")),
    el("span", "where", n.notebook === "trash" ? notebookName("trash") : notebookName(n.notebook)))));
  showPreview(el("header", "note-head",
    el("div", "head-row", el("h2", "", t("selectionCount", { n: ids.length })), actions),
    el("div", "meta", el("span", "", t("selectionHint")))), list);
}

// moveNotesTo moves the notes ids at once to notebook nb, or to the trash,
// and offers to undo it. As for one note, a note being edited is left alone,
// and moving notes of a private notebook where AI can read them is asked
// first.
async function moveNotesTo(ids, nb) {
  if (state.editing?.note && ids.includes(state.editing.note.id)) {
    alert(t("moveWhileEditing"));
    return;
  }
  const notes = ids.map(findListed).filter(Boolean);
  const target = state.notebooks.find((x) => x.id === nb);
  if (nb !== "trash" && target && target.kind !== "private" && notes.some((n) => noteKind(n) === "private") &&
      !confirm(t("confirmMovePrivateMany", { notebook: notebookName(nb) }))) return;
  try {
    if (nb === "trash") await api("/trash/notes", { method: "POST", body: { ids } });
    else await api("/notes", { method: "PATCH", body: { ids, notebook: nb } });
  } catch (e) {
    alert(t("moveToNotebookFailed", { error: e.message }));
    refreshAfterWrite();
    return;
  }
  if (nb === "trash") {
    offerUndo(t("toastTrashedMany", { n: ids.length }), async () => {
      for (const id of ids) await api(`${noteURL(id)}/restore`, { method: "POST" });
      refreshAfterWrite();
    });
    if (ids.includes(state.selected)) state.selected = null;
  } else {
    // Each goes back where it was; notes taken out of the trash do not.
    const back = {};
    for (const n of notes) {
      if (n.notebook !== "trash" && n.notebook !== nb) (back[n.notebook] ||= []).push(n.id);
    }
    if (Object.keys(back).length) {
      offerUndo(t("toastMovedMany", { n: ids.length, notebook: notebookName(nb) }), async () => {
        for (const [from, list] of Object.entries(back)) await api("/notes", { method: "PATCH", body: { ids: list, notebook: from } });
        refreshAfterWrite();
      });
    }
  }
  state.chosen.clear();
  refreshAfterWrite();
}

function moveSelection(delta) {
  const notes = listedNotes();
  if (!notes.length) return;
  const i = notes.findIndex((n) => n.id === state.selected);
  const next = i < 0 ? 0 : Math.min(Math.max(i + delta, 0), notes.length - 1);
  selectNote(notes[next].id);
}

// Preview

let noteSeq = 0;
let marks = [];
let markIndex = -1;

async function loadNote() {
  saveView();
  if (state.editing) return; // the editor keeps its own copy
  if (state.chosen.size > 1) {
    showChosen();
    return;
  }
  const seq = ++noteSeq;
  const pv = $("preview");
  const id = state.selected;
  if (!id) {
    state.note = null;
    showPreview(el("p", "empty", t("selectNote")));
    return;
  }
  let n;
  try {
    n = await api(noteURL(id), { params: { q: state.query } });
  } catch (e) {
    if (seq !== noteSeq || state.editing) return;
    state.note = null;
    showPreview(el("p", e.status === 404 ? "empty" : "error", e.status === 404 ? t("noteGone") : t("loadFailed", { error: e.message })));
    return;
  }
  if (seq !== noteSeq || state.editing) return;
  const keepScroll = pv.dataset.id === id ? pv.scrollTop : 0;
  state.note = n;
  const meta = el("div", "meta",
    el("span", "", notebookName(n.notebook) + (n.trashed_notebook ? " / " + trashedName(n.trashed_notebook) : "")
      + (n.trashed_from ? t("trashedFrom", { notebook: notebookName(n.trashed_from) }) : "")),
    el("span", "", t("updatedAt", { time: formatTime(n.updated_at) })),
    ...tagList(n.tags, true));
  const mark = aiMark(n, t("createdByAI"));
  if (mark) meta.append(mark);
  const actions = el("div", "actions", iconButton("find", t("findTitle"), () => (find.open ? closeFind() : openFind())), iconButton("document", t("copyTitle"), copyNote));
  if (n.notebook !== "trash") {
    const markButton = iconButton("flag", t("markTitle"), () => openMarkMenu(n, markButton));
    markButton.id = "mark-open";
    actions.prepend(markButton);
  }
  if (n.notebook === "trash") {
    actions.append(
      button(t("restore"), restoreNote, { title: t("restoreTitle") }),
      moveSelect((nb) => moveNoteTo(n.id, nb)),
      button(t("deletePermanently"), deleteNote, { cls: "danger" }));
  } else {
    actions.append(
      iconButton("edit", t("editTitle"), () => startEdit(n)),
      iconButton("history", t("historyTitle"), openHistory),
      iconButton("trash", t("toTrashTitle"), deleteNote));
  }
  const body = el("div", "body");
  renderBody(body, n.body || "", n.body_highlights);
  const title = el("h2", "", ...noteMarks(n), highlighted(n.title || t("untitled"), n.title_highlights));
  if (n.title) {
    const copy = button(kindSVG("copy"), copyTitle, { title: t("copyNoteTitle"), cls: "title-copy" });
    copy.setAttribute("aria-label", t("copyNoteTitle"));
    title.append(copy);
  }
  const link = button(kindSVG("link"), () => openLinkMenu(link), { title: t("copyLinkTitle"), cls: "title-copy" });
  link.id = "link-open";
  link.setAttribute("aria-label", t("copyLinkTitle"));
  title.append(link);
  // The find bar is drawn again with the note: the focus and the caret in it
  // stay, as the note may be drawn again while one types, such as on a change.
  const typing = document.activeElement?.id === "find-q" ? [$("find-q").selectionStart, $("find-q").selectionEnd] : null;
  showPreview(el("header", "note-head",
    el("div", "head-row", title, actions),
    meta, findBar()), body);
  if (typing) {
    $("find-q").focus();
    $("find-q").setSelectionRange(...typing);
  }
  pv.dataset.id = id;
  showNoteHash(id);
  marks = [...pv.querySelectorAll("mark:not(.find)")];
  markIndex = -1;
  if (marks.length) {
    const count = el("span", "", "");
    count.id = "match-count";
    meta.append(el("span", "matches", count,
      button("↑", () => jumpMark(-1), { title: t("prevMatch") }),
      button("↓", () => jumpMark(1), { title: t("nextMatch") })));
    updateMarkCount();
  }
  pv.scrollTop = keepScroll;
  if (find.open) runFind(false);
}

// Marks: a note has one at most for now, a color drawn as a dot or a symbol
// drawn as an emoji, to tell it at a glance. Their names, which say what they
// mean, are in the settings of the notes (PUT /marks), so that every machine
// shows the same. Set with the flag of the preview, a right click on a note in
// the list, or M.

const MARK_EMOJI = { star: "⭐", important: "❗", question: "❓", done: "✅", pin: "📌", urgent: "🔥", idea: "💡", wip: "🚧", hold: "⏳", dropped: "❌" };

// markLabel returns what mark id is called: its name, else what it looks like.
function markLabel(id) {
  return state.marks.find((m) => m.id === id)?.name || t("mark_" + id);
}

// markBadge returns the drawing of mark id, named for the tooltip and screen
// readers.
function markBadge(id) {
  const b = MARK_EMOJI[id] ? el("span", "mark-badge mark-symbol", MARK_EMOJI[id]) : el("span", `mark-badge mark-dot mark-${id}`);
  b.title = markLabel(id);
  b.setAttribute("role", "img");
  b.setAttribute("aria-label", t("markOf", { name: markLabel(id) }));
  return b;
}

// noteMarks returns the badges of the marks of note n that this page knows.
function noteMarks(n) {
  return (n.marks || []).filter((id) => state.marks.some((m) => m.id === id)).map(markBadge);
}

// openMarkMenu opens the menu of the marks for note n, beside the element at,
// or at the pointer for a right click.
function openMarkMenu(n, at, e) {
  const m = $("mark-menu");
  const cur = (n.marks || [])[0] ?? null;
  const pick = (id) => {
    closeMarkMenu();
    setMarks(n, id ? [id] : []);
  };
  const grid = (kind) => el("div", "mark-grid", ...state.marks.filter((mk) => mk.kind === kind).map((mk) => {
    const b = button(markBadge(mk.id), () => pick(mk.id === cur ? null : mk.id), { title: markLabel(mk.id), cls: mk.id === cur ? "active" : "" });
    b.setAttribute("role", "menuitemradio");
    b.setAttribute("aria-checked", String(mk.id === cur));
    b.setAttribute("aria-label", markLabel(mk.id));
    return b;
  }));
  const none = button(t("markNone"), () => pick(null), { cls: "mark-none" });
  none.setAttribute("role", "menuitem");
  m.replaceChildren(el("div", "menu-label", t("markMenu")), grid("color"), grid("symbol"), none);
  m.hidden = false;
  const r = at.getBoundingClientRect();
  const x = e ? e.clientX : r.right - m.offsetWidth;
  const y = e ? e.clientY : r.bottom + 4;
  m.style.left = Math.max(8, Math.min(x, innerWidth - m.offsetWidth - 8)) + "px";
  m.style.top = Math.min(y, innerHeight - m.offsetHeight - 8) + "px";
  (m.querySelector("button.active") || m.querySelector("button")).focus();
}

function closeMarkMenu() {
  $("mark-menu").hidden = true;
}

document.addEventListener("click", (e) => {
  if (!$("mark-menu").hidden && !e.target.closest("#mark-menu, #mark-open")) closeMarkMenu();
});
window.addEventListener("resize", closeMarkMenu);
$("mark-menu").addEventListener("keydown", (e) => {
  if (e.key === "Escape") {
    e.preventDefault();
    e.stopPropagation();
    closeMarkMenu();
  }
});

// setMarks sets the marks of note n, keeping its update time.
async function setMarks(n, marks) {
  try {
    await api(`${noteURL(n.id)}/marks`, { method: "PUT", body: { marks } });
  } catch (e) {
    alert(t("markFailed", { error: e.message }));
    return;
  }
  await loadNotes();
  if (state.selected === n.id && !state.editing) loadNote();
}

// openMarkNames opens the dialog to name the marks, which says what they mean.
function openMarkNames() {
  const d = $("mark-names");
  const inputs = {};
  const rows = state.marks.map((mk) => {
    const input = el("input");
    input.value = mk.name || "";
    input.placeholder = t("mark_" + mk.id);
    input.maxLength = 40;
    input.setAttribute("aria-label", t("mark_" + mk.id));
    inputs[mk.id] = input;
    return el("label", "mark-name-row", markBadge(mk.id), input);
  });
  const msg = el("p", "error");
  msg.hidden = true;
  const save = button(t("save"), async () => {
    const names = Object.fromEntries(Object.entries(inputs).map(([id, i]) => [id, i.value.trim()]));
    try {
      state.marks = await api("/marks", { method: "PUT", body: { names } });
    } catch (e) {
      msg.textContent = t("markFailed", { error: e.message });
      msg.hidden = false;
      return;
    }
    d.close();
    renderNotes();
    loadNote();
  }, { cls: "primary" });
  d.replaceChildren(
    el("div", "files-head", el("h2", "", t("markNames")), button("×", () => d.close(), { title: t("closeTitle") })),
    el("p", "hint", t("markNamesHint")),
    el("div", "mark-names-list", ...rows),
    msg,
    el("div", "files-actions", button(t("cancel"), () => d.close()), save));
  d.showModal();
  rows[0]?.querySelector("input").focus();
}

// Find in the note: a bar in the head of the preview, opened with f or its
// button (pressed again, it closes the bar), marks the text of the note that matches what is typed, without
// asking the server or changing the list, as the search box does. Enter and
// Shift+Enter, or n and N, go through the matches; Esc closes it. Text is
// compared in NFKC and lower case, so that full-width and half-width forms,
// and upper and lower case, match. The bar stays open from note to note.

const find = { open: false, q: "", marks: [], index: -1 };

// findBar returns the bar for the head of the preview, hidden unless open.
function findBar() {
  const input = el("input", "find-q");
  input.id = "find-q";
  input.type = "search";
  input.value = find.q;
  input.placeholder = t("findPlaceholder");
  input.setAttribute("aria-label", t("findPlaceholder"));
  input.autocomplete = "off";
  input.spellcheck = false;
  input.addEventListener("input", () => {
    find.q = input.value;
    runFind(true);
  });
  input.addEventListener("keydown", (e) => {
    e.stopPropagation(); // the keys of the page do not apply here
    if (e.isComposing || e.keyCode === 229) return; // Safari ends an IME conversion with keyCode 229
    if (e.key === "Enter") {
      e.preventDefault();
      jumpFind(e.shiftKey ? -1 : 1);
    } else if (e.key === "Escape") {
      e.preventDefault();
      closeFind();
    }
  });
  const count = el("span", "find-count", "");
  count.id = "find-count";
  const bar = el("div", "find-bar", kindSVG("find"), input, count,
    button("↑", () => jumpFind(-1), { title: t("prevMatch") }),
    button("↓", () => jumpFind(1), { title: t("nextMatch") }),
    button("×", closeFind, { title: t("closeTitle") }));
  bar.id = "find-bar";
  bar.hidden = !find.open;
  return bar;
}

function openFind() {
  const bar = $("find-bar");
  if (!bar) return; // no note shown
  find.open = true;
  bar.hidden = false;
  $("find-q").focus();
  $("find-q").select();
  runFind(false);
}

function closeFind() {
  find.open = false;
  clearFind();
  const bar = $("find-bar");
  if (bar) bar.hidden = true;
}

// clearFind takes the marks of the find away, putting the text back.
function clearFind() {
  for (const m of find.marks) {
    const parent = m.parentNode;
    if (!parent) continue;
    m.replaceWith(...m.childNodes);
    parent.normalize();
  }
  find.marks = [];
  find.index = -1;
}

// foldText returns s in NFKC and lower case, with, for each of its UTF-16
// units, the offset in s of the character it comes from.
function foldText(s) {
  let out = "";
  const at = [];
  for (let i = 0; i < s.length;) {
    const c = String.fromCodePoint(s.codePointAt(i));
    const f = c.normalize("NFKC").toLowerCase();
    for (let k = 0; k < f.length; k++) at.push(i);
    out += f;
    i += c.length;
  }
  at.push(s.length);
  return { text: out, at };
}

// runFind marks the matches of the find in the body of the note; with scroll,
// as when the text to find changes, it goes to the first one, and without,
// as when the note is drawn again, it stays at the match it was at.
function runFind(scroll) {
  const prev = find.index;
  clearFind();
  const body = $("preview").querySelector(".body");
  const q = find.q.normalize("NFKC").toLowerCase().trim();
  if (body && q) {
    const walker = document.createTreeWalker(body, NodeFilter.SHOW_TEXT, {
      // Not inside math, diagrams or code drawn by scripts.
      acceptNode: (n) => (n.parentElement?.closest(".katex, .math, .mermaid, svg") ? NodeFilter.FILTER_REJECT : NodeFilter.FILTER_ACCEPT),
    });
    const nodes = [];
    while (walker.nextNode()) nodes.push(walker.currentNode);
    for (const node of nodes) {
      const { text, at } = foldText(node.data);
      const ranges = [];
      for (let i = text.indexOf(q); i >= 0; i = text.indexOf(q, i + q.length)) ranges.push([at[i], at[i + q.length]]);
      // From the end, so that the offsets before stay right.
      for (const [from, to] of ranges.reverse()) {
        const range = document.createRange();
        range.setStart(node, from);
        range.setEnd(node, to);
        const m = el("mark", "find");
        range.surroundContents(m);
        find.marks.push(m);
      }
    }
    find.marks.sort((a, b) => (a.compareDocumentPosition(b) & Node.DOCUMENT_POSITION_FOLLOWING ? -1 : 1));
  }
  if (scroll && find.marks.length) jumpFind(1);
  else if (prev >= 0 && find.marks.length) {
    find.index = Math.min(prev, find.marks.length - 1);
    find.marks[find.index].classList.add("current");
  }
  updateFindCount();
}

function jumpFind(delta) {
  if (!find.marks.length) return;
  find.marks[find.index]?.classList.remove("current");
  find.index = (find.index + delta + find.marks.length) % find.marks.length;
  const m = find.marks[find.index];
  m.classList.add("current");
  m.scrollIntoView({ block: "center" });
  updateFindCount();
}

function updateFindCount() {
  const c = $("find-count");
  if (!c) return;
  c.textContent = !find.q.trim() ? "" : find.marks.length ? `${find.index < 0 ? "-" : find.index + 1} / ${find.marks.length}` : t("findNone");
}

// noteMarkdown returns note n as Markdown to copy: its title as a heading,
// then its body. The paths of its images are left as they are.
function noteMarkdown(n) {
  return n.title ? `# ${n.title}\n\n${n.body || ""}` : n.body || "";
}

// copyNote copies the note in the preview to the clipboard as Markdown.
async function copyNote() {
  const n = state.note;
  if (!n) return;
  try {
    await navigator.clipboard.writeText(noteMarkdown(n));
  } catch (e) {
    alert(t("copyFailed", { error: e.message }));
    return;
  }
  showToast(t("copiedMarkdown", { title: n.title || t("untitled") }));
}

// copyTitle copies the title of the note in the preview to the clipboard.
async function copyTitle() {
  const n = state.note;
  if (!n?.title) return;
  try {
    await navigator.clipboard.writeText(n.title);
  } catch (e) {
    alert(t("copyFailed", { error: e.message }));
    return;
  }
  showToast(t("copiedTitle", { title: n.title }));
}

// notePageURL returns the URL of note id in this UI, which opens the note here
// and which AI clients take to name the note.
function notePageURL(id) {
  return location.origin + "/#note/" + encodeURIComponent(id);
}

// showNoteHash shows the URL of note id in the address bar, or none, without
// adding to the history of the tab.
function showNoteHash(id) {
  const hash = id ? "#note/" + encodeURIComponent(id) : "";
  if (location.hash !== hash) history.replaceState(null, "", location.pathname + location.search + hash);
}

// copyLink copies a link to the note in the preview: its URL, or with
// markdown, a Markdown link with the title of the note.
async function copyLink(markdown) {
  const n = state.note;
  if (!n) return;
  const title = n.title || t("untitled");
  const text = markdown ? `[${title.replace(/[\\[\]]/g, "\\$&")}](${notePageURL(n.id)})` : notePageURL(n.id);
  try {
    await navigator.clipboard.writeText(text);
  } catch (e) {
    alert(t("copyFailed", { error: e.message }));
    return;
  }
  showToast(t(markdown ? "copiedMarkdownLink" : "copiedURL", { title }));
}

// openLinkMenu opens the menu of the links to copy below the button at.
function openLinkMenu(at) {
  const m = $("link-menu");
  if (!m.hidden) {
    closeLinkMenu();
    return;
  }
  const item = (label, markdown) => {
    const b = button(label, () => {
      closeLinkMenu();
      copyLink(markdown);
    });
    b.setAttribute("role", "menuitem");
    return b;
  };
  m.replaceChildren(item(t("copyURL"), false), item(t("copyMarkdownLink"), true));
  m.hidden = false;
  const r = at.getBoundingClientRect();
  m.style.left = Math.max(8, Math.min(r.left, innerWidth - m.offsetWidth - 8)) + "px";
  m.style.top = Math.min(r.bottom + 4, innerHeight - m.offsetHeight - 8) + "px";
  m.querySelector("button").focus();
}

function closeLinkMenu() {
  $("link-menu").hidden = true;
}

document.addEventListener("click", (e) => {
  if (!$("link-menu").hidden && !e.target.closest("#link-menu, #link-open")) closeLinkMenu();
});
window.addEventListener("resize", closeLinkMenu);
$("link-menu").addEventListener("keydown", (e) => {
  if (e.key === "Escape") {
    e.preventDefault();
    e.stopPropagation();
    closeLinkMenu();
  }
});

function showPreview(...children) {
  const pv = $("preview");
  pv.classList.remove("editing");
  pv.replaceChildren(...children);
  delete pv.dataset.id;
  showNoteHash(null);
  marks = [];
  markIndex = -1;
}

function jumpMark(delta) {
  if (!marks.length) return;
  marks[markIndex]?.classList.remove("current");
  markIndex = (markIndex + delta + marks.length) % marks.length;
  const m = marks[markIndex];
  m.classList.add("current");
  m.scrollIntoView({ block: "center" });
  updateMarkCount();
}

function updateMarkCount() {
  const c = $("match-count");
  if (c) c.textContent = `${markIndex < 0 ? "-" : markIndex + 1} / ${marks.length}`;
}

// Delete and restore. Both send the version of the note in the preview, so a
// note changed elsewhere meanwhile is not deleted unseen.

async function deleteNote() {
  const n = state.note;
  if (!n) return;
  if (n.notebook === "trash" && !confirm(t("confirmDelete", { title: n.title }))) return;
  try {
    await api(noteURL(n.id), { method: "DELETE", ifMatch: n.version });
  } catch (e) {
    actionFailed(e);
    return;
  }
  if (n.notebook !== "trash") offerUndo(t("toastTrashed", { title: n.title || t("untitled") }), () => undoTrash(n.id));
  const listed = listedNotes();
  const i = listed.findIndex((x) => x.id === n.id);
  const next = listed[i + 1] || listed[i - 1];
  state.selected = next ? next.id : null;
  refreshAfterWrite();
}

async function restoreNote() {
  const n = state.note;
  if (!n || n.notebook !== "trash") return;
  try {
    await api(`${noteURL(n.id)}/restore`, { method: "POST", ifMatch: n.version });
  } catch (e) {
    actionFailed(e);
    return;
  }
  refreshAfterWrite();
}

function actionFailed(e) {
  if (e.status === 412) {
    alert(t("changedReloaded"));
  } else {
    alert(t("actionFailed", { error: e.message }));
  }
  loadNote();
}

function refreshAfterWrite() {
  markSelected();
  loadNote();
  loadNotebooks().catch(() => {});
  loadNotes();
}

// Editor. state.editing holds the note being edited (null for a new note) and
// the values loaded into the form, which tell whether there are unsaved
// changes.

// ID_PATTERN is the pattern of note IDs in the API design.
const ID_PATTERN = /^[a-z0-9][a-z0-9_-]{0,63}$/;

function parseTags(s) {
  const seen = new Set();
  return s.split(/[,\u3001]/) // ASCII or ideographic commas
    .map((tag) => tag.trim()).filter((tag) => tag && !seen.has(tag) && seen.add(tag));
}

// The editor of the body is CodeMirror 6 (vendor/codemirror.js, the global CM,
// built by internal/ui/web/codemirror), loaded on the first edit.
let editorReady = null;
let editorLoading = false;

function loadEditor() {
  editorReady ||= loadScript("assets/vendor/codemirror.js").then(() => {
    // With EditContext, Chrome on macOS shows the candidates of an IME at the
    // top left of the page on a new line; contenteditable has no such problem.
    CM.EditorView.EDIT_CONTEXT = false;
  });
  return editorReady;
}

// bodyExtensions returns the extensions of the editor of the body: Markdown
// with its code blocks colored, Tab and Shift+Tab to indent, Enter to continue
// a list or a quote, undo history, and images pasted or dropped.
function bodyExtensions() {
  const markup = CM.tagHighlighter([
    { tag: CM.tags.monospace, class: "tok-monospace" },
    { tag: CM.tags.quote, class: "tok-quote" },
    { tag: [CM.tags.processingInstruction, CM.tags.contentSeparator], class: "tok-mark" },
  ]);
  return [
    CM.history(),
    CM.markdown({ base: CM.markdownLanguage, codeLanguages: CM.codeLanguages }),
    // Tab takes the completion offered, before it indents.
    CM.keymap.of([{ key: "Tab", run: CM.acceptCompletion }, ...CM.defaultKeymap, ...CM.historyKeymap, CM.indentWithTab]),
    CM.autocompletion({
      override: [emojiCompletion],
      icons: false,
      addToOptions: [{ render: (c) => el("span", "cm-emoji", c.emoji), position: 20 }],
    }),
    CM.syntaxHighlighting(CM.classHighlighter),
    CM.syntaxHighlighting(markup),
    // Long lines are not wrapped: the editor scrolls sideways instead.
    CM.placeholder(t("bodyPlaceholder")),
    CM.EditorView.contentAttributes.of({ spellcheck: "false", "aria-label": t("bodyPlaceholder") }),
    CM.EditorView.domEventHandlers({
      paste(e) {
        if (!hasFiles(e.clipboardData) || !addImages(e.clipboardData.files)) return false;
        e.preventDefault();
        return true;
      },
      dragover(e) {
        if (!hasFiles(e.dataTransfer)) return false;
        e.preventDefault();
        return true;
      },
      drop(e, view) {
        if (!hasFiles(e.dataTransfer)) return false;
        e.preventDefault();
        const pos = view.posAtCoords({ x: e.clientX, y: e.clientY });
        if (pos !== null) view.dispatch({ selection: { anchor: pos } });
        view.focus();
        addImages(e.dataTransfer.files);
        return true;
      },
    }),
  ];
}

// Emoji completion: ":" and two letters or more of a name, such as :smi,
// offer the emoji whose names (or tags, after them) start so, from the list of
// gemoji in vendor/emoji.json, read with the first one. The emoji chosen goes
// in as the character itself, so that notes read the same everywhere. Not in
// code or a URL, nor after a letter, a digit, a slash or a colon, as in
// http:// or 12:30. The emoji used most in this browser come first.

let emojiList = null;

function loadEmoji() {
  emojiList ||= fetch("assets/vendor/emoji.json")
    .then((r) => (r.ok ? r.json() : { emoji: [] }))
    .then((d) => d.emoji || [])
    .catch(() => []);
  return emojiList;
}

function emojiUsed() {
  try {
    return JSON.parse(localStorage.getItem("ebira.emoji") || "{}") || {};
  } catch (_) {
    return {};
  }
}

function countEmoji(emoji) {
  const used = emojiUsed();
  used[emoji] = (used[emoji] || 0) + 1;
  try { localStorage.setItem("ebira.emoji", JSON.stringify(used)); } catch (_) { /* not kept */ }
}

async function emojiCompletion(context) {
  const word = context.matchBefore(/:[a-z0-9_+-]{2,}/i);
  if (!word) return null;
  if (/[\w/:]/.test(context.state.sliceDoc(word.from - 1, word.from))) return null;
  for (let n = CM.syntaxTree(context.state).resolveInner(context.pos, -1); n; n = n.parent) {
    if (/Code|URL|Autolink/.test(n.name)) return null;
  }
  const list = await loadEmoji();
  const used = emojiUsed();
  const apply = (emoji) => (view, _c, from, to) => {
    view.dispatch({ changes: { from, to, insert: emoji }, selection: { anchor: from + emoji.length } });
    countEmoji(emoji);
  };
  // Among the names that match, the emoji used more come first, then the
  // shorter names (:smile: before :smile_cat:), then the names before tags.
  const options = [];
  for (const [emoji, aliases, tags = []] of list) {
    const usedBoost = Math.min(50, (used[emoji] || 0) * 10);
    const boost = (name, tag) => usedBoost - Math.min(30, name.length) - (tag ? 15 : 0);
    for (const a of aliases) options.push({ label: `:${a}:`, emoji, boost: boost(a, false), apply: apply(emoji) });
    for (const tg of tags) options.push({ label: `:${tg}:`, detail: aliases[0], emoji, boost: boost(tg, true), apply: apply(emoji) });
  }
  return { from: word.from, options, validFor: /^:[a-z0-9_+-]*$/i };
}

// editHistories keeps, for the notes last edited, the state of the editor of
// the body with its undo history when it was saved or closed, so that editing
// the note again can undo past the save. It is used only while the note is at
// the version the history ends at; the page keeps it in memory only.
const editHistories = new Map(); // note ID -> {version, json}
const EDIT_HISTORIES = 20;

// rememberHistory keeps the history of the editor ed for the note id at
// version, if the body is that version's.
function rememberHistory(ed, id, version, body) {
  if (!id || !version || ed.view.state.doc.toString() !== body) return;
  editHistories.delete(id);
  editHistories.set(id, { version, json: ed.view.state.toJSON({ history: CM.historyField }) });
  while (editHistories.size > EDIT_HISTORIES) editHistories.delete(editHistories.keys().next().value);
}

// bodyState returns the state of the editor of the body of note: the one kept
// with its history if the note is still at its version, or a new one.
function bodyState(note) {
  const h = note && editHistories.get(note.id);
  if (h && h.version === note.version && h.json.doc === note.body) {
    try {
      return CM.EditorState.fromJSON(h.json, { extensions: bodyExtensions() }, { history: CM.historyField });
    } catch (e) {
      editHistories.delete(note.id);
    }
  }
  return CM.EditorState.create({ doc: note ? note.body : "", extensions: bodyExtensions() });
}

function startEdit(note) {
  if (state.editing || editorLoading) return;
  if (note && note.notebook === "trash") return;
  if (!window.CM) {
    editorLoading = true;
    loadEditor().then(() => {
      editorLoading = false;
      startEdit(note);
    }, (e) => {
      editorLoading = false;
      editorReady = null;
      alert(t("actionFailed", { error: e.message }));
    });
    return;
  }
  const defaultNotebook = state.notebook && state.notebook !== "trash" ? state.notebook : "inbox";
  const ed = {
    note, // null for a new note
    version: note ? note.version : null,
    elems: {},
    preview: false,
  };
  state.editing = ed;
  noteSeq++; // drop a preview being loaded

  const f = ed.elems;
  f.title = el("input", "title");
  f.title.placeholder = t("title");
  f.title.value = note ? note.title : "";
  f.notebook = el("select");
  for (const nb of state.notebooks) {
    if (nb.id === "trash") continue;
    const o = el("option", "", notebookName(nb.id) + (kindIcon(nb.kind) ? " " + kindIcon(nb.kind) : ""));
    o.value = nb.id;
    f.notebook.append(o);
  }
  f.notebook.value = note ? note.notebook : defaultNotebook;
  f.tags = el("input");
  f.tags.placeholder = t("tagsPlaceholder");
  f.tags.value = note ? note.tags.join(", ") : state.tag || ""; // a new note under a tag gets it
  f.body = el("div", "editor-body");
  ed.view = new CM.EditorView({ parent: f.body, state: bodyState(note) });
  f.rendered = el("div", "body editor-preview");
  f.rendered.hidden = true;
  f.banner = el("div", "banner");
  f.banner.hidden = true;
  f.msg = el("span", "msg");
  f.toggle = button(t("preview"), togglePreview, { title: t("previewTitle") });

  const fields = el("div", "fields",
    el("label", "", el("span", "", t("notebook")), f.notebook),
    el("label", "grow", el("span", "", t("tags")), f.tags));
  if (!note) {
    f.id = el("input");
    f.id.placeholder = t("idPlaceholder");
    f.id.title = t("idTitle");
    fields.append(el("label", "", el("span", "", "ID"), f.id));
  } else {
    fields.append(el("span", "note-id", `ID: ${note.id}`));
  }
  const form = el("form", "editor",
    el("div", "editor-head",
      f.title,
      fields,
      el("div", "editor-actions", f.msg, f.toggle,
        button(t("cancel"), cancelEdit, { title: t("cancelTitle") }),
        el("button", "primary", t("save")))),
    f.banner, f.body, f.rendered);
  form.querySelector("button.primary").type = "submit";
  form.querySelector("button.primary").title = t("saveTitle");
  form.addEventListener("submit", (e) => { e.preventDefault(); saveNote(); });
  // Enter in a field does not submit the form, which would save and close the
  // note before its body is written: in the title it goes on to the body, in
  // the other fields it does nothing. Cmd+S or Ctrl+S and Save save.
  for (const input of [f.title, f.tags, f.id].filter(Boolean)) {
    input.addEventListener("keydown", (e) => {
      if (e.key !== "Enter" || e.isComposing || e.keyCode === 229) return; // Safari ends an IME conversion with keyCode 229
      e.preventDefault();
      if (input === f.title) ed.view.focus();
    });
  }

  const pv = $("preview");
  pv.replaceChildren(form);
  pv.classList.add("editing");
  delete pv.dataset.id;
  showNoteHash(note?.id);
  ed.loaded = formValues();
  (note ? ed.view : f.title).focus();
}

function formValues() {
  const f = state.editing.elems;
  return {
    title: f.title.value.trim(),
    notebook: f.notebook.value,
    tags: parseTags(f.tags.value),
    body: state.editing.view.state.doc.toString(),
    id: f.id ? f.id.value.trim() : "",
  };
}

function isDirty() {
  if (!state.editing) return false;
  return JSON.stringify(formValues()) !== JSON.stringify(state.editing.loaded);
}

// leaveEditor closes the editor, asking first if there are unsaved changes. It
// returns false if the user keeps editing.
function leaveEditor() {
  if (!state.editing) return true;
  if (isDirty() && !confirm(t("confirmDiscard"))) return false;
  endEdit();
  return true;
}

// endEdit drops the editor, which the caller replaces. An editor left
// unchanged keeps its history for the next edit of the note.
function endEdit() {
  const ed = state.editing;
  if (!ed) return;
  if (ed.note) rememberHistory(ed, ed.note.id, ed.version, ed.note.body);
  ed.view.destroy();
  state.editing = null;
}

function cancelEdit() {
  if (!leaveEditor()) return;
  applySelectionCheck();
  loadNote();
}

function togglePreview() {
  const ed = state.editing;
  const f = ed.elems;
  ed.preview = !ed.preview;
  if (ed.preview) renderBody(f.rendered, ed.view.state.doc.toString(), null);
  f.body.hidden = ed.preview;
  f.rendered.hidden = !ed.preview;
  f.toggle.textContent = t(ed.preview ? "editBody" : "preview");
  if (!ed.preview) ed.view.focus();
}

// hasFiles reports whether a paste or a drop carries files.
function hasFiles(dt) {
  return !!dt && [...dt.types].includes("Files");
}

// addImages uploads the images among files to the note being edited and
// inserts references to them at the cursor. It returns false when there is no
// image, so that the paste goes on as usual. A new note has no files until it
// is saved.
function addImages(files) {
  const ed = state.editing;
  const images = [...files].filter((f) => f.type.startsWith("image/"));
  if (!ed || !images.length) return false;
  if (!ed.note) {
    editorMessage(t("imageSaveFirst"), true);
    return true;
  }
  uploadImages(ed, images);
  return true;
}

async function uploadImages(ed, images) {
  editorMessage(t("imageUploading", { n: images.length }));
  const refs = [];
  const errors = [];
  for (const file of images) {
    const name = file.name || "image.png";
    try {
      const res = await api(`${noteURL(ed.note.id)}/files`, { method: "POST", params: { name }, raw: file });
      const alt = res.name.replace(/\.[^.]+$/, "");
      refs.push(`![${alt}](${res.path})`);
    } catch (e) {
      errors.push(t("imageFailed", { name, error: e.message }));
    }
  }
  if (state.editing !== ed) return; // closed meanwhile; the files stay with the note
  if (refs.length) {
    const view = ed.view;
    const { from, to } = view.state.selection.main;
    const sep = from > 0 && view.state.sliceDoc(from - 1, from) !== "\n" ? "\n" : "";
    const insert = sep + refs.join("\n") + "\n";
    view.dispatch({ changes: { from, to, insert }, selection: { anchor: from + insert.length }, scrollIntoView: true });
  }
  editorMessage(errors.join(" ") || t("imageAdded", { n: refs.length }), errors.length > 0);
}

function editorMessage(text, isError) {
  const msg = state.editing?.elems.msg;
  if (!msg) return;
  msg.textContent = text;
  msg.classList.toggle("error", !!isError);
}

let saving = false;

async function saveNote(overwrite = false) {
  const ed = state.editing;
  if (!ed || saving) return;
  const v = formValues();
  if (!v.title) {
    editorMessage(t("titleRequired"), true);
    ed.elems.title.focus();
    return;
  }
  if (v.id && !ID_PATTERN.test(v.id)) {
    editorMessage(t("idInvalid"), true);
    ed.elems.id.focus();
    return;
  }
  saving = true;
  editorMessage(t("saving"));
  let saved;
  try {
    if (!ed.note) {
      saved = await api("/notes", { method: "POST", body: { id: v.id || undefined, notebook: v.notebook, title: v.title, tags: v.tags, body: v.body } });
    } else {
      const patch = { title: v.title, tags: v.tags, body: v.body };
      if (v.notebook !== ed.note.notebook) patch.notebook = v.notebook;
      saved = await api(noteURL(ed.note.id), { method: "PATCH", body: patch, ifMatch: overwrite ? undefined : ed.version });
    }
  } catch (e) {
    saving = false;
    await saveFailed(e);
    return;
  }
  saving = false;
  if (state.editing !== ed) return; // closed meanwhile
  rememberHistory(ed, saved.id, saved.version, saved.body);
  ed.note = null; // the history is kept as of the save
  endEdit();
  state.selected = saved.id;
  refreshAfterWrite();
}

async function saveFailed(e) {
  const ed = state.editing;
  if (!ed) return;
  if (e.status === 412) {
    editorMessage("");
    const choice = await askConflict();
    if (choice === "overwrite") {
      saveNote(true);
    } else if (choice === "reload") {
      endEdit();
      try {
        startEdit(await api(noteURL(ed.note.id)));
      } catch (err) {
        loadNote();
      }
    }
    return;
  }
  if (e.status === 404 && ed.note) {
    editorMessage(t("deletedWhileEditing"), true);
    return;
  }
  if (e.status === 409) {
    editorMessage(t("idTaken"), true);
    return;
  }
  editorMessage(t("saveFailed", { error: e.message }), true);
}

// Check of the files of notes: the files no note refers to, which can be
// deleted, and the references to files that do not exist.

function formatSize(n) {
  if (n >= 1 << 20) return `${(n / (1 << 20)).toFixed(1)} MB`;
  if (n >= 1 << 10) return `${(n / (1 << 10)).toFixed(1)} KB`;
  return `${n} B`;
}

// noteAnchor returns a link that opens note id and closes the dialog d.
function noteAnchor(id, title, d) {
  const a = el("a", "", title || t("untitled"));
  a.href = `/#note/${encodeURIComponent(id)}`;
  a.addEventListener("click", () => d.close());
  return a;
}

// History: the earlier versions of the note in the preview, kept on this
// device by the server. Putting one back saves its content as the note, so the
// content replaced goes into the history too.

async function openHistory() {
  const n = state.note;
  if (!n) return;
  const d = $("history");
  const head = el("div", "files-head", el("h2", "", t("historyHeading", { title: n.title || t("untitled") })),
    button("\u00d7", () => d.close(), { title: t("closeTitle") }));
  d.replaceChildren(head, el("p", "", t("historyLoading")));
  if (!d.open) d.showModal();
  let versions;
  try {
    versions = await api(`${noteURL(n.id)}/history`);
  } catch (e) {
    d.replaceChildren(head, el("p", "error", t("loadFailed", { error: e.message })));
    return;
  }
  if (!versions.length) {
    d.replaceChildren(head, el("p", "empty", t("historyEmpty")));
    return;
  }
  const list = el("ul", "history-list");
  const view = el("div", "history-view");
  let seq = 0;
  const show = async (v, li) => {
    for (const x of list.children) x.classList.toggle("active", x === li);
    const my = ++seq;
    view.replaceChildren(el("p", "", t("historyLoading")));
    let full;
    try {
      full = await api(`${noteURL(n.id)}/history/${encodeURIComponent(v.rev)}`);
    } catch (e) {
      if (my === seq) view.replaceChildren(el("p", "error", t("loadFailed", { error: e.message })));
      return;
    }
    if (my === seq) renderVersion(view, n, full, d);
  };
  for (const v of versions) {
    const li = el("li", "", el("span", "history-time", formatTime(v.replaced_at, true)), el("span", "history-name", v.title || t("untitled")));
    li.tabIndex = 0;
    li.title = t("historyUpdatedAt", { time: formatTime(v.updated_at) });
    li.addEventListener("click", () => show(v, li));
    li.addEventListener("keydown", (e) => {
      if (e.key === "Enter") show(v, li);
    });
    list.append(li);
  }
  d.replaceChildren(head, el("p", "history-note", t("historyNote")), el("div", "history-body", list, view));
  show(versions[0], list.firstChild);
}

// renderVersion shows the version v of note n in view: what putting it back
// would change, and the button to do so.
function renderVersion(view, n, v, d) {
  const parts = [];
  if (v.title !== n.title) parts.push(el("p", "history-change", t("historyTitleChange", { from: n.title, to: v.title })));
  if (v.tags.join(", ") !== n.tags.join(", ")) {
    parts.push(el("p", "history-change", t("historyTagsChange", { from: n.tags.join(", ") || "-", to: v.tags.join(", ") || "-" })));
  }
  const diff = lineDiff((n.body || "").split("\n"), (v.body || "").split("\n"));
  if (diff.some((x) => x.op !== " ")) {
    const pre = el("pre", "history-diff");
    for (const x of contextOf(diff, 3)) {
      pre.append(x.op === "…" ? el("span", "skip", "\u22ef\n") : el("span", x.op === "+" ? "add" : x.op === "-" ? "del" : "", `${x.op} ${x.line}\n`));
    }
    parts.push(el("p", "history-legend", t("historyDiffLegend")), pre);
  }
  if (!parts.length) {
    view.replaceChildren(el("p", "empty", t("historySame")));
    return;
  }
  view.replaceChildren(...parts, el("div", "files-actions",
    button(t("historyRestore"), () => restoreVersion(n, v, d), { cls: "primary" })));
}

async function restoreVersion(n, v, d) {
  if (!confirm(t("historyRestoreConfirm", { time: formatTime(v.replaced_at, true) }))) return;
  try {
    await api(noteURL(n.id), { method: "PATCH", body: { title: v.title, tags: v.tags, body: v.body }, ifMatch: n.version });
  } catch (e) {
    d.close();
    actionFailed(e);
    return;
  }
  d.close();
  refreshAfterWrite();
}

// lineDiff returns the lines of a and b as a list of {op, line}: " " for a
// line of both, "-" for a line of a only and "+" for one of b only, by the
// longest common subsequence of the lines between the common start and end.
// A middle too long to compare is shown as removed and added as a whole.
function lineDiff(a, b) {
  let start = 0;
  while (start < a.length && start < b.length && a[start] === b[start]) start++;
  let end = 0;
  while (end < a.length - start && end < b.length - start && a[a.length - 1 - end] === b[b.length - 1 - end]) end++;
  const x = a.slice(start, a.length - end);
  const y = b.slice(start, b.length - end);
  const out = a.slice(0, start).map((line) => ({ op: " ", line }));
  if (x.length * y.length > 4e6) {
    out.push(...x.map((line) => ({ op: "-", line })), ...y.map((line) => ({ op: "+", line })));
  } else {
    // lcs[i][j] is the length of the common subsequence of x[i:] and y[j:].
    const lcs = Array.from({ length: x.length + 1 }, () => new Uint32Array(y.length + 1));
    for (let i = x.length - 1; i >= 0; i--) {
      for (let j = y.length - 1; j >= 0; j--) {
        lcs[i][j] = x[i] === y[j] ? lcs[i + 1][j + 1] + 1 : Math.max(lcs[i + 1][j], lcs[i][j + 1]);
      }
    }
    let i = 0, j = 0;
    while (i < x.length || j < y.length) {
      if (i < x.length && j < y.length && x[i] === y[j]) {
        out.push({ op: " ", line: x[i++] });
        j++;
      } else if (j < y.length && (i === x.length || lcs[i][j + 1] >= lcs[i + 1][j])) {
        out.push({ op: "+", line: y[j++] });
      } else {
        out.push({ op: "-", line: x[i++] });
      }
    }
  }
  out.push(...a.slice(a.length - end).map((line) => ({ op: " ", line })));
  return out;
}

// contextOf keeps the changed lines of diff and n lines around them, putting
// {op: "…"} in place of the lines left out.
function contextOf(diff, n) {
  const keep = diff.map(() => false);
  diff.forEach((x, i) => {
    if (x.op === " ") return;
    for (let k = Math.max(0, i - n); k <= Math.min(diff.length - 1, i + n); k++) keep[k] = true;
  });
  const out = [];
  diff.forEach((x, i) => {
    if (keep[i]) out.push(x);
    else if (keep[i - 1] !== false || i === 0) out.push({ op: "…" });
  });
  return out.filter((x, i) => x.op !== "…" || out[i - 1]?.op !== "…");
}

async function openFilesCheck(message) {
  const d = $("files-check");
  d.replaceChildren(el("p", "", t("filesChecking")));
  if (!d.open) d.showModal();
  let res;
  try {
    res = await api("/files/check");
  } catch (e) {
    d.replaceChildren(filesCheckHead(d), el("p", "error", t("filesCheckFailed", { error: e.message })));
    return;
  }
  renderFilesCheck(d, res, message);
}

function filesCheckHead(d) {
  return el("div", "files-head", el("h2", "", t("filesCheck")), button("×", () => d.close(), { title: t("closeTitle") }));
}

function renderFilesCheck(d, res, message) {
  const parts = [filesCheckHead(d)];
  if (message) parts.push(el("p", message.error ? "error" : "msg", message.text));

  const unused = el("section", "", el("h3", "", t("filesUnused", { n: res.unused.length })));
  const boxes = [];
  if (!res.unused.length) {
    unused.append(el("p", "empty", t("filesNoUnused")));
  } else {
    const ul = el("ul", "files-list");
    for (const f of res.unused) {
      const cb = el("input");
      cb.type = "checkbox";
      cb.checked = !f.recent; // a recent file may belong to a note being edited
      boxes.push({ cb, f });
      const img = el("img", "thumb");
      img.src = `${noteURL(f.note_id)}/files/${encodeURIComponent(f.name)}`;
      img.alt = f.name;
      img.loading = "lazy";
      const meta = el("span", "files-meta", `${formatSize(f.size)} · ${formatTime(f.added_at)} · `, noteAnchor(f.note_id, f.title, d));
      if (f.recent) meta.append(" · ", el("span", "recent", t("filesRecent")));
      ul.append(el("li", "", el("label", "", cb, img, el("span", "files-info", el("span", "files-name", f.name), meta))));
    }
    unused.append(ul, el("div", "files-actions", button(t("filesDelete"), () => deleteUnusedFiles(boxes), { cls: "primary" })));
  }
  parts.push(unused);

  const broken = el("section", "", el("h3", "", t("filesBroken", { n: res.broken.length })));
  if (!res.broken.length) {
    broken.append(el("p", "empty", t("filesNoBroken")));
  } else {
    const ul = el("ul", "files-list broken");
    for (const b of res.broken) ul.append(el("li", "", el("code", "", b.path), " · ", noteAnchor(b.note_id, b.title, d)));
    broken.append(ul);
  }
  parts.push(broken);
  d.replaceChildren(...parts);
}

async function deleteUnusedFiles(boxes) {
  const chosen = boxes.filter((b) => b.cb.checked).map((b) => b.f);
  if (!chosen.length || !confirm(t("filesConfirmDelete", { n: chosen.length }))) return;
  const errors = [];
  for (const f of chosen) {
    try {
      await api(`${noteURL(f.note_id)}/files/${encodeURIComponent(f.name)}`, { method: "DELETE" });
    } catch (e) {
      errors.push(t("filesDeleteFailed", { path: f.path, error: e.message }));
    }
  }
  const deleted = chosen.length - errors.length;
  openFilesCheck(errors.length
    ? { text: [t("filesDeleted", { n: deleted }), ...errors].join(" "), error: true }
    : { text: t("filesDeleted", { n: deleted }) });
}

function askConflict() {
  const d = $("conflict");
  d.returnValue = "cancel";
  d.showModal();
  return new Promise((resolve) => d.addEventListener("close", () => resolve(d.returnValue), { once: true }));
}

// noteChangedElsewhere tells the editor that its note was changed or deleted
// by someone else. Saving then asks what to do (412).
function noteChangedElsewhere(type) {
  const ed = state.editing;
  if (!ed || !ed.note) return;
  const b = ed.elems.banner;
  b.textContent = type === "deleted"
    ? t("bannerDeleted")
    : t("bannerChanged");
  b.hidden = false;
}

// A file dropped outside the editor would make the browser leave the page to
// show it. Markdown files dropped there are imported into the notebook shown.
for (const type of ["dragover", "drop"]) {
  window.addEventListener(type, (e) => {
    if (!hasFiles(e.dataTransfer) || e.defaultPrevented) return;
    e.preventDefault();
    if (type === "drop") importMarkdown(e.dataTransfer.files, importNotebook());
  });
}

// Import of Markdown files: dropped on the page or on a notebook, or chosen
// from the settings menu. Images chosen with them are sent along when a file
// refers to them by name.

const MARKDOWN_FILE = /\.(md|markdown)$/i;

// importNotebook returns the notebook to import into: the one shown, else
// the inbox.
function importNotebook() {
  return state.notebook && state.notebook !== "trash" && !state.tag ? state.notebook : "inbox";
}

// acceptImport lets Markdown files be dropped on the notebook row li. The
// trash takes none.
function acceptImport(li, id) {
  li.addEventListener("dragover", (e) => {
    if (!hasFiles(e.dataTransfer)) return;
    e.preventDefault();
    if (id === "trash") {
      e.dataTransfer.dropEffect = "none";
      return;
    }
    li.classList.add("drop-into");
  });
  li.addEventListener("dragleave", () => li.classList.remove("drop-into"));
  li.addEventListener("drop", (e) => {
    li.classList.remove("drop-into");
    if (!hasFiles(e.dataTransfer)) return;
    e.preventDefault();
    if (id !== "trash") importMarkdown(e.dataTransfer.files, id);
  });
}

// acceptNote lets a note dragged from the list be dropped on the notebook row
// li, which moves it there; dropped on the trash, it goes to the trash.
function acceptNote(li, id) {
  const target = () => {
    if (!dragID) return false;
    return (draggedNotes() ?? [dragID]).some((x) => {
      const n = findListed(x);
      return !!n && n.notebook !== id;
    });
  };
  li.addEventListener("dragover", (e) => {
    if (!target()) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = "move";
    li.classList.add("drop-into");
  });
  li.addEventListener("dragleave", () => li.classList.remove("drop-into"));
  li.addEventListener("drop", (e) => {
    if (!target()) return;
    e.preventDefault();
    li.classList.remove("drop-into");
    const many = draggedNotes();
    if (many) moveNotesTo(many, id);
    else moveNoteTo(dragID, id);
  });
}

// moveNoteTo moves note id to notebook nb, or to the trash. A note being
// edited is left alone, as the editor would save it back.
async function moveNoteTo(id, nb) {
  if (state.editing?.note?.id === id) {
    alert(t("moveWhileEditing"));
    return;
  }
  const n = findListed(id) || (state.note?.id === id ? state.note : null);
  const title = n?.title || t("untitled");
  const target = state.notebooks.find((x) => x.id === nb);
  if (n && nb !== "trash" && noteKind(n) === "private" && target && target.kind !== "private" &&
      !confirm(t("confirmMovePrivate", { title, notebook: target.name }))) return;
  try {
    if (nb === "trash") {
      await api(noteURL(id), { method: "DELETE" });
    } else {
      await api(noteURL(id), { method: "PATCH", body: { notebook: nb } });
    }
    if (nb === "trash") {
      offerUndo(t("toastTrashed", { title }), () => undoTrash(id));
    } else if (n?.notebook === "trash") {
      // Out of the trash: it cannot go back where it was, such as into a
      // notebook in the trash, so there is no undo; it can be deleted again.
      state.selected = null;
    } else if (n) {
      offerUndo(t("toastMoved", { title, notebook: notebookName(nb) }), async () => {
        await api(noteURL(id), { method: "PATCH", body: { notebook: n.notebook } });
        refreshAfterWrite();
      });
    }
  } catch (e) {
    alert(t("moveToNotebookFailed", { error: e.message }));
  }
  if (state.selected === id && nb === "trash") state.selected = null;
  refreshAfterWrite();
}

// noteKind returns the kind of the notebook note n belongs to: for a note in
// the trash, the notebook it was deleted from or with.
function noteKind(n) {
  if (n.trashed_notebook) return state.trashed.find((x) => x.id === n.trashed_notebook)?.kind;
  return state.notebooks.find((x) => x.id === (n.trashed_from || n.notebook))?.kind;
}

// moveSelect returns a menu of the notebooks to move notes to, such as a note
// in the trash; pick is called with the notebook chosen.
function moveSelect(pick) {
  const sel = el("select", "move-to");
  sel.title = t("moveToTitle");
  sel.setAttribute("aria-label", t("moveToTitle"));
  const first = el("option", "", t("moveTo"));
  first.value = "";
  sel.append(first);
  for (const nb of state.notebooks) {
    if (nb.id === "trash") continue;
    const o = el("option", "", notebookName(nb.id) + (kindIcon(nb.kind) ? " " + kindIcon(nb.kind) : ""));
    o.value = nb.id;
    sel.append(o);
  }
  sel.addEventListener("change", () => {
    const nb = sel.value;
    sel.value = "";
    if (nb) pick(nb);
  });
  return sel;
}

// Undo of the operations on notes and notebooks: after one, a bar offers to
// undo it for a while, also with Cmd+Z or Ctrl+Z outside the fields. Undoing
// runs the opposite operation; it offers no undo of its own.
let undoAction = null;
let undoTimer = 0;
const UNDO_MS = 10000;

function offerUndo(message, fn) {
  undoAction = fn;
  const bar = $("toast");
  bar.querySelector(".toast-msg").textContent = message;
  bar.querySelector("button").hidden = false;
  bar.hidden = false;
  clearTimeout(undoTimer);
  undoTimer = setTimeout(hideToast, UNDO_MS);
}

// showToast shows message in the bar for a moment, without an undo.
function showToast(message) {
  undoAction = null;
  const bar = $("toast");
  bar.querySelector(".toast-msg").textContent = message;
  bar.querySelector("button").hidden = true;
  bar.hidden = false;
  clearTimeout(undoTimer);
  undoTimer = setTimeout(hideToast, 3000);
}

function hideToast() {
  $("toast").hidden = true;
  undoAction = null;
}

async function runUndo() {
  const fn = undoAction;
  if (!fn) return;
  undoAction = null;
  const bar = $("toast");
  bar.querySelector("button").hidden = true;
  clearTimeout(undoTimer);
  try {
    await fn();
    bar.querySelector(".toast-msg").textContent = t("undone");
  } catch (e) {
    bar.querySelector(".toast-msg").textContent = t("undoFailed", { error: e.message });
  }
  undoTimer = setTimeout(hideToast, 3000);
}

$("toast").querySelector("button").addEventListener("click", runUndo);

// undoTrash takes note id back out of the trash and shows it.
async function undoTrash(id) {
  await api(`${noteURL(id)}/restore`, { method: "POST" });
  state.selected = id;
  refreshAfterWrite();
}

// imageRefs returns the relative paths of the images a Markdown file refers
// to, as internal/importer finds them.
function imageRefs(md) {
  const refs = new Set();
  for (const re of [/!\[[^\]]*\]\(\s*(<[^>\n]*>|[^)\s]+)/g, /<img\b[^>]*?\bsrc\s*=\s*["']([^"']+)/gi]) {
    for (const m of md.matchAll(re)) {
      const p = m[1].replace(/^<|>$/g, "");
      if (p && !/^[a-zA-Z][a-zA-Z0-9+.-]*:/.test(p) && !/^[/#\\]/.test(p)) refs.add(p);
    }
  }
  return [...refs];
}

function fileBase64(file) {
  return new Promise((resolve, reject) => {
    const r = new FileReader();
    r.onload = () => resolve(String(r.result).replace(/^[^,]*,/, ""));
    r.onerror = () => reject(r.error);
    r.readAsDataURL(file);
  });
}

function baseName(ref) {
  const name = ref.split("/").pop();
  try {
    return decodeURIComponent(name);
  } catch (_) {
    return name;
  }
}

// importMarkdown imports the Markdown files among files into notebook and
// shows the outcome. It returns false when there is no Markdown file.
async function importMarkdown(files, notebook) {
  const all = [...files];
  const docs = all.filter((f) => MARKDOWN_FILE.test(f.name));
  if (!docs.length) return false;
  const images = all.filter((f) => f.type.startsWith("image/"));
  const d = $("import-result");
  d.replaceChildren(el("p", "", t("importing", { n: docs.length })));
  if (!d.open) d.showModal();
  const results = [];
  for (const f of docs) {
    let content;
    try {
      content = new TextDecoder("utf-8", { fatal: true }).decode(await f.arrayBuffer());
    } catch (_) {
      results.push({ file: f.name, error: t("importNotUTF8") });
      continue;
    }
    try {
      const sent = [];
      for (const ref of imageRefs(content)) {
        const img = images.find((i) => i.name === baseName(ref));
        if (img) sent.push({ ref, name: img.name, content: await fileBase64(img) });
      }
      const modified = new Date(f.lastModified).toISOString().replace(/\.\d+Z$/, "Z");
      const res = await api("/import", { method: "POST", body: { notebook, name: f.name, content, modified_at: modified, files: sent } });
      results.push({ file: f.name, res });
    } catch (e) {
      results.push({ file: f.name, error: e.message });
    }
  }
  renderImportResults(d, results, notebook);
  loadNotebooks().catch(() => {});
  loadNotes();
  return true;
}

function renderImportResults(d, results, notebook) {
  const count = (pred) => results.filter(pred).length;
  const created = count((r) => r.res?.status === "created");
  const skipped = count((r) => r.res?.status === "skipped");
  const head = el("div", "files-head", el("h2", "", t("importTitle")), button("×", () => d.close(), { title: t("closeTitle") }));
  const summary = el("p", "", t("importSummary", { notebook: notebookName(notebook), created, skipped, failed: results.length - created - skipped }));
  const ul = el("ul", "files-list import-list");
  for (const r of results) {
    const li = el("li", "", el("span", "files-name", r.file), " · ");
    if (r.error) {
      li.append(el("span", "failed", t("importFailed", { error: r.error })));
    } else if (r.res.status === "created") {
      li.append(t("importCreated"), " ", noteAnchor(r.res.note.id, r.res.note.title, d));
    } else {
      li.append(t("importSkipped"), " ", noteAnchor(r.res.duplicate_of, t("importExisting"), d));
    }
    const warnings = r.res?.warnings || [];
    if (warnings.length) li.append(el("ul", "warnings", ...warnings.map((w) => el("li", "", w))));
    ul.append(li);
  }
  d.replaceChildren(head, summary, ul);
}

// showGitSync shows Sync with git in the settings menu when the notes are in
// a git repository; git is optional.
async function showGitSync() {
  try {
    $("git-sync-open").hidden = !(await api("/git/status")).repository;
  } catch (_) { /* left hidden */ }
}

// syncGit syncs the notes with their git repository (POST /git/sync) and
// shows the outcome. The changed notes come back as change events.
async function syncGit() {
  const d = $("git-sync");
  const head = el("div", "files-head", el("h2", "", t("gitSync")), button("×", () => d.close(), { title: t("closeTitle") }));
  d.replaceChildren(head, el("p", "", t("gitSyncing")));
  if (!d.open) d.showModal();
  let res;
  try {
    res = await api("/git/sync", { method: "POST", body: {} });
  } catch (e) {
    d.replaceChildren(head, el("p", "error", t("gitSyncFailed", { error: e.message })));
    return;
  }
  const parts = [head, el("p", "", t("gitCommitted", { n: res.committed }))];
  if (res.upstream) {
    parts.push(el("p", "", t("gitMerged", { upstream: res.upstream, n: res.merged })));
    parts.push(el("p", "", t(res.pushed ? "gitPushed" : "gitNothingToPush", { upstream: res.upstream })));
  } else {
    parts.push(el("p", "msg", t("gitNoUpstream")));
  }
  if (res.conflicts.length) {
    const ul = el("ul", "files-list");
    for (const c of res.conflicts) {
      const li = el("li", "", c.note_id ? noteAnchor(c.note_id, c.path, d) : el("span", "files-name", c.path), " · ");
      if (c.copy_id) {
        li.append(t("gitConflictCopy"), " ", noteAnchor(c.copy_id, t("gitConflictCopyNote"), d));
      } else {
        li.append(t("gitConflictKept"));
      }
      ul.append(li);
    }
    parts.push(el("section", "", el("h3", "", t("gitConflicts", { n: res.conflicts.length })), ul));
  }
  if (res.errors.length) {
    parts.push(el("section", "", el("h3", "", t("gitUnreadable", { n: res.errors.length })),
      el("ul", "files-list", ...res.errors.map((e) => el("li", "", e)))));
  }
  d.replaceChildren(...parts);
}

window.addEventListener("beforeunload", (e) => {
  if (isDirty()) e.preventDefault();
});

$("new-note").addEventListener("click", () => {
  if (leaveEditor()) startEdit(null);
});

// Assistant: questions to POST /ask. Each question is answered on its own; the
// questions and answers are kept in the page only.

let askController = null; // aborts the question being answered

function toggleAssistant(open = $("assistant").hidden) {
  $("assistant").hidden = !open;
  document.querySelector(".panes").classList.toggle("with-assistant", open);
  $("ask-toggle").classList.toggle("active", open);
  fitColumns();
  if (open) {
    renderAskEmpty();
    $("ask-q").focus();
    checkAssistant();
  } else if ($("assistant").contains(document.activeElement)) {
    document.activeElement.blur();
  }
}

function renderAskEmpty() {
  const log = $("ask-log");
  if (!log.children.length) {
    log.append(el("p", "empty", t("askEmpty")));
  }
}

// checkAssistant asks the server, each time the assistant opens, whether it
// can answer questions; when it cannot, the reason replaces the guide and the
// question box is disabled. The server may have been restarted with an API key
// meanwhile, so the answer is not kept.
async function checkAssistant() {
  let s;
  try {
    s = await api("/ask");
  } catch (_) {
    return; // unknown, such as with an older server: the question itself tells
  }
  const log = $("ask-log");
  log.querySelector(".ask-unavailable")?.remove();
  $("ask-q").disabled = !s.available;
  $("ask-send").disabled = !s.available || !!askController;
  if (s.available) {
    renderAskEmpty();
    if (!$("assistant").hidden && !$("assistant").contains(document.activeElement)) $("ask-q").focus();
    return;
  }
  log.querySelector(".empty")?.remove();
  log.append(el("p", "error ask-unavailable", t("askUnavailable", { error: s.reason || "" })));
}

async function ask(question) {
  if (askController) return;
  const log = $("ask-log");
  log.querySelector(".empty")?.remove();
  const controller = new AbortController();
  askController = controller;
  const status = el("span", "", t("askPending"));
  const answer = el("div", "ask-answer", el("div", "ask-pending", status, button(t("abort"), () => controller.abort())));
  log.append(el("div", "ask-question", question), answer);
  answer.scrollIntoView({ block: "nearest" });
  const started = Date.now();
  const timer = setInterval(() => {
    status.textContent = t("askPendingSeconds", { seconds: Math.round((Date.now() - started) / 1000) });
  }, 1000);
  $("ask-send").disabled = true;
  try {
    const r = await api("/ask", { method: "POST", body: { question }, signal: controller.signal });
    const body = el("div", "body");
    renderBody(body, r.answer || "", null);
    answer.replaceChildren(body);
  } catch (e) {
    const msg = e.name === "AbortError" ? t("aborted")
      : e.status === 503 ? t("askUnavailable", { error: e.message })
      : t("askFailed", { error: e.message });
    answer.replaceChildren(el("p", e.name === "AbortError" ? "empty" : "error", msg));
    if (!$("ask-q").value) $("ask-q").value = question; // to ask again
  } finally {
    clearInterval(timer);
    askController = null;
    $("ask-send").disabled = $("ask-q").disabled;
  }
}

$("ask-toggle").addEventListener("click", () => toggleAssistant());
// Settings menu: the gear at the bottom of the left column.

// The style of the preview: a built-in style written to preview.css, next to
// the settings file, where the user may change it (see GET and PUT
// /preview-style). The dialog tries the style chosen on the note open by
// swapping the stylesheet of the preview, and puts it back when it closes.

const PREVIEW_STYLES = ["default", "band", "document", "compact", "lined"];

// styleName is the name of a built-in style shown to people.
const styleName = (s) => t("style" + s[0].toUpperCase() + s.slice(1));

// tryPreviewStyle shows style in the preview without writing it; null puts
// back preview.css, read again.
function tryPreviewStyle(style) {
  const link = $("preview-css");
  link.disabled = style === "default";
  if (style === null) link.href = `assets/preview.css?v=${Date.now()}`;
  else if (style !== "default") link.href = `assets/styles/${style}.css`;
}

async function openPreviewStyle() {
  const d = $("preview-style");
  const head = el("div", "files-head", el("h2", "", t("previewStyleHead")), button("×", () => d.close(), { title: t("closeTitle") }));
  d.replaceChildren(head);
  if (!d.open) d.showModal();
  let cur;
  try {
    cur = await api("/preview-style");
  } catch (e) {
    d.append(el("p", "error", t("previewStyleFailed", { error: e.message })));
    return;
  }
  const now = cur.style === "" ? t("previewStyleOwn")
    : cur.modified ? t("previewStyleModified", { style: styleName(cur.style) }) : styleName(cur.style);
  const list = el("fieldset", "styles");
  for (const s of PREVIEW_STYLES) {
    const radio = el("input");
    radio.type = "radio";
    radio.name = "preview-style";
    radio.value = s;
    radio.checked = s === cur.style;
    radio.addEventListener("change", () => tryPreviewStyle(s));
    list.append(el("label", "", radio, el("span", "", el("b", "", styleName(s)), " ", el("span", "desc", t("style" + s[0].toUpperCase() + s.slice(1) + "Desc")))));
  }
  const msg = el("p", "error");
  msg.hidden = true;
  const use = button(t("previewStyleUse"), async () => {
    const style = list.querySelector("input:checked")?.value;
    if (!style) return;
    // A file not a built-in style as it is goes to the backup: say so first.
    if ((cur.style === "" || cur.modified) && !confirm(t("previewStyleConfirm", { path: cur.path, backup: cur.path + ".bak" }))) return;
    let res;
    try {
      res = await api("/preview-style", { method: "PUT", body: { style } });
    } catch (e) {
      msg.textContent = t("previewStyleFailed", { error: e.message });
      msg.hidden = false;
      return;
    }
    d.close();
    showToast(t("previewStyleDone", { style: styleName(style) }) + (res.backup ? " " + t("previewStyleBackedUp", { backup: res.backup }) : ""));
  }, { cls: "primary" });
  d.replaceChildren(head,
    el("p", "now", t("previewStyleNow", { style: now })),
    list,
    el("p", "hint", t("previewStyleTry")),
    el("p", "hint", t("previewStyleFile", { path: cur.path })),
    msg,
    el("div", "files-actions", button(t("cancel"), () => d.close()), use));
  (list.querySelector("input:checked") || list.querySelector("input")).focus();
}

$("preview-style").addEventListener("close", () => tryPreviewStyle(null));

function toggleSettings(open = $("settings-menu").hidden) {
  $("settings-menu").hidden = !open;
  $("settings").setAttribute("aria-expanded", String(open));
  if (open) $("settings-menu").querySelector("button").focus();
}

$("settings").addEventListener("click", () => toggleSettings());
$("settings-menu").addEventListener("click", (e) => {
  if (e.target.closest("button")) toggleSettings(false);
});
document.addEventListener("click", (e) => {
  if (!$("settings-menu").hidden && !e.target.closest(".side-foot")) toggleSettings(false);
});
$("settings-menu").addEventListener("keydown", (e) => {
  if (e.key === "Escape") {
    e.preventDefault();
    e.stopPropagation();
    toggleSettings(false);
    $("settings").focus();
  }
});
$("files-check-open").addEventListener("click", () => openFilesCheck());
$("mark-names-open").addEventListener("click", () => openMarkNames());
$("preview-style-open").addEventListener("click", () => openPreviewStyle());
$("git-sync-open").addEventListener("click", () => syncGit());
// Theme: auto (the system's), light or dark, chosen in the settings menu and
// kept in the browser; without a choice, the server's (see theme.js).
function currentTheme() {
  let chosen = null;
  try { chosen = localStorage.getItem("ebira.theme"); } catch (_) { /* none */ }
  return ["auto", "light", "dark"].includes(chosen) ? chosen : document.documentElement.dataset.serverTheme || "auto";
}

function markTheme() {
  const cur = currentTheme();
  for (const b of document.querySelectorAll("#settings-menu [data-theme-choice]")) {
    b.setAttribute("aria-checked", String(b.dataset.themeChoice === cur));
  }
}

function setTheme(choice) {
  try { localStorage.setItem("ebira.theme", choice); } catch (_) { /* this page only */ }
  const root = document.documentElement;
  if (choice === "auto") delete root.dataset.theme;
  else root.dataset.theme = choice;
  markTheme();
  // Diagrams are drawn in the colors of the theme; draw them again.
  mermaidReady = null;
  if (!state.editing && state.note) loadNote();
}

for (const b of document.querySelectorAll("#settings-menu [data-theme-choice]")) {
  b.addEventListener("click", () => setTheme(b.dataset.themeChoice));
}
markTheme();

for (const b of document.querySelectorAll("#settings-menu [data-lang]")) {
  b.addEventListener("click", () => {
    if (b.dataset.lang === lang) return;
    try { localStorage.setItem("ebira.lang", b.dataset.lang); } catch (_) { /* this page only */ }
    location.reload();
  });
}
$("import-open").addEventListener("click", () => $("import-input").click());
$("import-input").addEventListener("change", (e) => {
  const files = [...e.target.files];
  e.target.value = ""; // the same files can be chosen again
  importMarkdown(files, importNotebook());
});
$("ask-close").addEventListener("click", () => toggleAssistant(false));
$("ask-clear").addEventListener("click", () => {
  askController?.abort();
  $("ask-log").replaceChildren();
  renderAskEmpty();
  checkAssistant();
});
$("ask-form").addEventListener("submit", (e) => {
  e.preventDefault();
  const q = $("ask-q").value.trim();
  if (!q || askController || $("ask-q").disabled) return;
  $("ask-q").value = "";
  ask(q);
});
$("ask-q").addEventListener("keydown", (e) => {
  // Enter sends, Shift+Enter breaks the line; not while an IME converts.
  if (e.key === "Enter" && !e.shiftKey && !e.isComposing && e.keyCode !== 229) {
    e.preventDefault();
    $("ask-form").requestSubmit();
  }
});

// A name of a notebook or a tag cut off by the width of the column shows in
// full on hover. The title is set on hover, as the width changes with the
// window and the name.
document.addEventListener("mouseover", (e) => {
  const name = e.target.closest?.(".notebooks .name");
  if (!name) return;
  if (name.scrollWidth > name.clientWidth) name.title = name.textContent;
  else name.removeAttribute("title");
});

// Links to notes (/#note/<ID>), in answers or in notes, open the note in the
// preview. The same URL opened from elsewhere does too.
document.addEventListener("click", (e) => {
  const a = e.target.closest?.("a[href]");
  if (!a || e.metaKey || e.ctrlKey || e.shiftKey) return;
  const id = noteLink(a.getAttribute("href"));
  if (!id) return;
  e.preventDefault();
  selectNote(id);
});

// The address bar shows the URL of the note in the preview (showNoteHash):
// one pasted there opens its note, and the address bar shows the note in the
// preview again until it does.
function openLinkedNote() {
  const id = noteLink(location.href);
  if (!id) return;
  showNoteHash(state.note?.id);
  selectNote(id);
}

window.addEventListener("hashchange", openLinkedNote);

// Search

let searchTimer;

// The clear button shows while the search box has text.
function showSearchClear() {
  $("q-clear").hidden = !$("q").value;
}

// The help of the search syntax opens below the box with "?", and closes
// with "?" again, Esc or a click elsewhere.
function toggleSearchHelp(open = $("search-help").hidden) {
  $("search-help").hidden = !open;
  $("q-help").setAttribute("aria-expanded", String(open));
}

$("q-help").addEventListener("click", () => toggleSearchHelp());
document.addEventListener("click", (e) => {
  if (!$("search-help").hidden && !e.target.closest("#search-help, #q-help")) toggleSearchHelp(false);
});
document.addEventListener("keydown", (e) => {
  if (e.key === "Escape" && !$("search-help").hidden) {
    e.stopPropagation();
    toggleSearchHelp(false);
  }
}, true);

$("q-clear").addEventListener("click", () => {
  $("q").value = "";
  $("q").dispatchEvent(new Event("input"));
  $("q").focus();
});

$("q").addEventListener("input", () => {
  showSearchClear();
  suggestTags();
  clearTimeout(searchTimer);
  searchTimer = setTimeout(() => {
    const q = searchQuery();
    if (q === state.query) return;
    state.query = q;
    // The search box finds notes: the first one found is shown. To find
    // within the note shown, there is the find bar of the preview.
    loadNotes({ chosen: true, first: true }).then(() => loadNote()); // refresh the highlights
  }, 250);
});

$("search-form").addEventListener("submit", (e) => e.preventDefault());

// Tag completion: while the word at the cursor starts with tag:, the tags
// that start with what follows are offered below the box, those of the
// notebook shown (all of them for all notes). Arrows choose one, Enter or Tab
// puts it in, Esc closes the list. The tag being typed is left out of the
// search until it is complete, so that the list does not empty meanwhile.

// TAG_AT_CURSOR matches a tag being typed at the end of the text before the
// cursor: tag:name, or tag:"name for one with spaces, after a minus to leave
// the tag out.
const TAG_AT_CURSOR = /(^|\s)(-?)tag:(?:"([^"]*)|([^\s"]*))$/i;

// tagKey compares tags as the server does: NFKC and lower case.
const tagKey = (s) => s.normalize("NFKC").toLowerCase();

let suggested = []; // the tags offered
let suggestIndex = -1; // the one chosen with the arrows
let notebookTags = { notebook: null, tags: [] }; // the tags of the notebook shown

// typedTag returns the tag being typed at the cursor: where its word starts
// (at tag:, after the minus), whether a minus is before it, and the text
// after tag:; or null.
function typedTag() {
  const q = $("q");
  const before = q.value.slice(0, q.selectionStart ?? q.value.length);
  const m = TAG_AT_CURSOR.exec(before);
  if (!m) return null;
  return { start: m.index + m[1].length + m[2].length, minus: m[2] !== "", prefix: m[3] ?? m[4] ?? "" };
}

// tagChoices returns the tags to offer: those of the notebook shown, else all.
async function tagChoices() {
  const nb = state.notebook && state.notebook !== "trash" ? state.notebook : null;
  if (!nb) return state.tags;
  if (notebookTags.notebook !== nb) {
    try {
      notebookTags = { notebook: nb, tags: await api("/tags", { params: { notebook: nb } }) };
    } catch (_) {
      return state.tags;
    }
  }
  return notebookTags.tags;
}

// searchQuery returns the query to search: the box, without a tag being
// typed unless a tag of that name exists.
function searchQuery() {
  const q = $("q").value;
  const typed = typedTag();
  if (!typed || state.tags.some((tg) => tagKey(tg.name) === tagKey(typed.prefix))) return q.trim();
  return (q.slice(0, typed.start - (typed.minus ? 1 : 0)) + q.slice($("q").selectionStart ?? q.length)).trim();
}

async function suggestTags() {
  const typed = typedTag();
  if (!typed) {
    hideTagSuggest();
    return;
  }
  const key = tagKey(typed.prefix);
  const tags = (await tagChoices())
    .filter((tg) => tagKey(tg.name).startsWith(key))
    .sort((a, b) => b.count - a.count || a.name.localeCompare(b.name))
    .slice(0, 10);
  if (typedTag()?.start !== typed.start) return; // typed on meanwhile
  suggested = tags;
  suggestIndex = tags.length ? 0 : -1;
  renderTagSuggest();
}

function renderTagSuggest() {
  const ul = $("tag-suggest");
  ul.replaceChildren(...suggested.map((tg, i) => {
    const li = el("li", "", el("span", "name", tg.name), el("span", "count", String(tg.count)));
    li.id = `tag-suggest-${i}`;
    li.setAttribute("role", "option");
    li.setAttribute("aria-selected", String(i === suggestIndex));
    li.addEventListener("mousedown", (e) => e.preventDefault()); // keep the focus in the box
    li.addEventListener("click", () => putTag(tg.name));
    return li;
  }));
  ul.hidden = !suggested.length;
  $("q").setAttribute("aria-expanded", String(!ul.hidden));
  if (suggestIndex >= 0) $("q").setAttribute("aria-activedescendant", `tag-suggest-${suggestIndex}`);
  else $("q").removeAttribute("aria-activedescendant");
}

function hideTagSuggest() {
  suggested = [];
  suggestIndex = -1;
  renderTagSuggest();
}

// putTag puts tag in place of the tag being typed, quoted when it has spaces,
// followed by a space, and searches.
function putTag(tag) {
  const typed = typedTag();
  if (!typed) return;
  const q = $("q");
  const end = q.selectionStart ?? q.value.length;
  const word = /\s|"/.test(tag) ? `tag:"${tag.replaceAll('"', "")}" ` : `tag:${tag} `;
  // The rest of the word after the cursor is part of the tag being typed.
  const inQuotes = q.value.slice(typed.start + 4, typed.start + 5) === '"';
  const rest = q.value.slice(end).replace(inQuotes ? /^[^"]*"?/ : /^\S*/, "").replace(/^\s/, "");
  q.value = q.value.slice(0, typed.start) + word + rest;
  const caret = typed.start + word.length;
  q.setSelectionRange(caret, caret);
  hideTagSuggest();
  q.dispatchEvent(new Event("input"));
}

$("q").addEventListener("keydown", (e) => {
  if (!suggested.length || e.isComposing || e.keyCode === 229) return; // Safari ends an IME conversion with keyCode 229
  const move = { ArrowDown: 1, ArrowUp: -1 }[e.key];
  if (move) {
    suggestIndex = (suggestIndex + move + suggested.length) % suggested.length;
    renderTagSuggest();
  } else if ((e.key === "Enter" || e.key === "Tab") && suggestIndex >= 0) {
    putTag(suggested[suggestIndex].name);
  } else if (e.key === "Escape") {
    hideTagSuggest();
  } else {
    return;
  }
  // The keys go to the list, not to the search box or the page.
  e.preventDefault();
  e.stopPropagation();
});
$("q").addEventListener("blur", hideTagSuggest);
$("q").addEventListener("click", suggestTags); // the cursor moved

// Keys: / search, Esc clear, j/k or arrows move, n/N jump between matches,
// e edit, c create, u restore, s sort, J/K move the note in the manual order,
// a assistant, F2 rename the notebook; in the editor, ⌘S / Ctrl+S save and Esc cancel; in the
// assistant, Esc leaves the question box and then closes it.

document.addEventListener("keydown", (e) => {
  if ($("conflict").open || $("files-check").open || $("history").open || $("import-result").open || $("notebook-new").open || $("preview-style").open || $("mark-names").open) return;
  if ($("assistant").contains(e.target)) {
    if (e.key === "Escape" && !e.isComposing) {
      e.preventDefault();
      e.target.blur();
    }
    return;
  }
  if (state.editing) {
    if ((e.metaKey || e.ctrlKey) && e.key === "s") {
      e.preventDefault();
      saveNote();
    } else if (e.key === "Escape" && !e.isComposing) {
      e.preventDefault();
      cancelEdit();
    }
    return;
  }
  if ((e.metaKey || e.ctrlKey) && !e.shiftKey && !e.altKey && e.key === "z" && undoAction && !e.target.closest("input, textarea, select")) {
    e.preventDefault();
    runUndo();
    return;
  }
  if ((e.metaKey || e.ctrlKey) && !e.shiftKey && !e.altKey && e.key === "a" && !e.target.closest("input, textarea, select, [contenteditable]")) {
    e.preventDefault();
    chooseAll();
    return;
  }
  if (e.metaKey || e.ctrlKey || e.altKey || e.isComposing) return;
  if (document.activeElement === $("q")) {
    if (e.key === "Escape") {
      $("q").value = "";
      $("q").dispatchEvent(new Event("input"));
      $("q").blur();
    } else if (e.key === "ArrowDown" || e.key === "Enter") {
      e.preventDefault();
      $("q").blur();
      moveSelection(state.selected ? 0 : 1);
    }
    return;
  }
  if (e.target.closest("input, textarea, select, button, dialog")) return;
  switch (e.key) {
    case "/": e.preventDefault(); $("q").focus(); $("q").select(); break;
    case "j": case "ArrowDown": e.preventDefault(); moveSelection(1); break;
    case "k": case "ArrowUp": e.preventDefault(); moveSelection(-1); break;
    case "n": if (find.open && find.marks.length) jumpFind(1); else jumpMark(1); break;
    case "N": if (find.open && find.marks.length) jumpFind(-1); else jumpMark(-1); break;
    case "f": if (state.note && !state.editing) { e.preventDefault(); openFind(); } break;
    case "Y": if (state.note && !state.editing && $("link-open")) { e.preventDefault(); openLinkMenu($("link-open")); } break;
    case "M": if (state.note && state.note.notebook !== "trash" && !state.editing && $("mark-open")) { e.preventDefault(); openMarkMenu(state.note, $("mark-open")); } break;
    case "J": e.preventDefault(); moveSelected(1); break;
    case "K": e.preventDefault(); moveSelected(-1); break;
    case "s": e.preventDefault(); if (!state.query) cycleSort(); break;
    case "e": if (state.note) { e.preventDefault(); startEdit(state.note); } break;
    case "c": e.preventDefault(); startEdit(null); break;
    case "u": restoreNote(); break;
    case "y": if (state.note && !state.editing) { e.preventDefault(); copyNote(); } break;
    case "a": e.preventDefault(); toggleAssistant(true); break;
    case "T": e.preventDefault(); setSide(state.side === "tags" ? "notebooks" : "tags"); break;
    case "F2":
      if (state.side === "notebooks" && state.notebook && !state.trashedNotebook) {
        e.preventDefault();
        renameNotebook(state.notebook);
      }
      break;
    case "Escape":
      if (find.open) closeFind();
      else if (state.chosen.size) clearChosen();
      else if (!$("assistant").hidden) toggleAssistant(false);
      break;
  }
});

// Change events: reload what may have changed. Events come in bursts, such as
// on a git pull, so reloading is debounced.

let reloadTimer;
let reloadNote = false;

function scheduleReload(event) {
  const id = event?.id;
  if (state.editing && state.editing.note && id === state.editing.note.id) {
    noteChangedElsewhere(event.type);
  }
  if (!event || id === undefined || id === state.selected) reloadNote = true;
  clearTimeout(reloadTimer);
  reloadTimer = setTimeout(async () => {
    const note = reloadNote;
    reloadNote = false;
    try {
      await loadNotebooks();
    } catch (_) { /* shown by the list */ }
    loadNotes();
    if (note) loadNote();
  }, 200);
}

function connect() {
  const es = new EventSource("/events");
  const status = $("status");
  // The state is told by the color of the dot and by words, as in the TUI.
  const show = (cls, text, title) => {
    status.className = `status ${cls}`;
    status.querySelector(".status-text").textContent = t(text);
    status.title = t(title);
  };
  es.addEventListener("open", () => show("ok", "statusLive", "eventsConnected"));
  es.addEventListener("error", () => show("ng", "statusOffline", "eventsReconnecting"));
  es.addEventListener("ready", () => scheduleReload());
  es.addEventListener("notebooks", () => scheduleReload({ type: "notebooks", id: null }));
  for (const type of ["created", "updated", "deleted"]) {
    es.addEventListener(type, (e) => {
      let id = null;
      try { id = JSON.parse(e.data).id ?? null; } catch (_) { /* no ID */ }
      scheduleReload({ type, id });
    });
  }
}

// Passkeys: with auth: passkey, the REST API treats only requests signed in
// with a passkey as from people, and the others as from AI, which cannot see
// private notebooks. The page asks to sign in before loading the notes. A
// passkey is registered on the one-time URL that ebira auth passkey prints,
// #register/<token>.

let authState = { mode: "none", registered: false, signed_in: false };

function showAuthState() {
  const mode = t(authState.mode === "passkey" ? "authModePasskey" : "authModeNone");
  $("auth-state").textContent = t(authState.signed_in ? "authStateSignedIn" : "authState", { mode });
  $("logout").hidden = !authState.signed_in;
  $("passkeys-open").hidden = !authState.signed_in;
}

const passkeysSupported = () => window.PublicKeyCredential
  && typeof PublicKeyCredential.parseCreationOptionsFromJSON === "function"
  && typeof PublicKeyCredential.parseRequestOptionsFromJSON === "function";

// authDialog shows the auth dialog with a title, a message (text or an
// element) and buttons; it returns the element for errors.
function authDialog(title, message, ...buttons) {
  const d = $("auth");
  const msg = el("p", "error");
  const body = typeof message === "string" ? el("p", "", message) : message;
  d.replaceChildren(el("div", "files-head", el("h2", "", title)), body, msg, el("div", "files-actions", ...buttons));
  if (!d.open) d.showModal();
  buttons.at(-1)?.focus();
  return msg;
}

// The sign-in cannot be skipped: without it the REST API refuses the requests
// of the page.
$("auth").addEventListener("cancel", (e) => {
  if ($("auth").dataset.required) e.preventDefault();
});

let signingIn = false;

function showSignIn(message) {
  if (signingIn) return;
  signingIn = true;
  $("auth").dataset.required = "1";
  if (!authState.registered) {
    showNoPasskey();
    return;
  }
  showSignInButton(message);
}

// registerCommand is the command that registers a passkey, in a terminal.
const registerCommand = "ebira auth passkey";

// showNoPasskey tells how to register a passkey when none is, and moves on to
// the sign-in by itself once one is: it asks the server every few seconds
// while the dialog is open.
function showNoPasskey() {
  const copy = button(t("authCopy"), async () => {
    try {
      await navigator.clipboard.writeText(registerCommand);
      copy.textContent = t("authCopied");
    } catch (_) {
      copy.disabled = true; // the command stays selectable
    }
  });
  authDialog(t("authTitle"), el("div", "",
    el("p", "", t("authNoPasskey")),
    el("div", "auth-cmd", el("code", "", registerCommand), copy),
    el("p", "msg", t("authWaiting"))));
  const timer = setInterval(async () => {
    let st;
    try {
      st = await api("/auth");
    } catch (_) {
      return; // the server may be restarting; ask again
    }
    if (st.mode !== "passkey" || st.signed_in) {
      clearInterval(timer);
      location.reload();
    } else if (st.registered) {
      clearInterval(timer);
      authState = st;
      showSignInButton();
    }
  }, 3000);
}

function showSignInButton(message) {
  let msg;
  const go = button(t("authSignIn"), async () => {
    go.disabled = true;
    msg.textContent = "";
    try {
      await signIn();
      location.reload();
    } catch (e) {
      msg.textContent = passkeyError(e);
      go.disabled = false;
    }
  }, { cls: "primary" });
  msg = authDialog(t("authTitle"), message || t("authSignInText"), go);
}

async function signIn() {
  if (!passkeysSupported()) throw new Error(t("authUnsupported"));
  const c = await api("/auth/passkey/login/begin", { method: "POST" });
  const cred = await navigator.credentials.get({ publicKey: PublicKeyCredential.parseRequestOptionsFromJSON(c.options) });
  await api("/auth/passkey/login/finish", { method: "POST", body: { ceremony: c.ceremony, credential: cred.toJSON() } });
}

function showRegister(token) {
  let msg;
  const close = button(t("cancel"), () => $("auth").close());
  const go = button(t("authRegister"), async () => {
    go.disabled = true;
    msg.textContent = "";
    try {
      if (!passkeysSupported()) throw new Error(t("authUnsupported"));
      const c = await api("/auth/passkey/register/begin", { method: "POST", body: { token } });
      const cred = await navigator.credentials.create({ publicKey: PublicKeyCredential.parseCreationOptionsFromJSON(c.options) });
      await api("/auth/passkey/register/finish", { method: "POST", body: { ceremony: c.ceremony, credential: cred.toJSON() } });
    } catch (e) {
      // The token is used once; a new one is needed after a failure.
      msg.textContent = e.status === 403 ? t("authTokenInvalid") : passkeyError(e);
      if (e.status !== 403 && e.status !== 400) go.disabled = false;
      return;
    }
    const done = button(t("authDone"), () => location.reload(), { cls: "primary" });
    authDialog(t("authRegisterTitle"), t(authState.mode === "passkey" ? "authRegistered" : "authRegisteredNone"), done);
  }, { cls: "primary" });
  msg = authDialog(t("authRegisterTitle"), t("authRegisterText"), close, go);
}

// verifyPasskey asks to sign in with the passkey again, for an important
// operation, such as making a private notebook visible to AI or emptying the
// trash; the new session counts as verified for a few minutes. It fails when
// people cancel.
function verifyPasskey() {
  return new Promise((resolve, reject) => {
    const d = $("auth");
    delete d.dataset.required;
    let msg;
    const done = (err) => {
      d.removeEventListener("close", onClose);
      d.close();
      if (err) reject(err); else resolve();
    };
    const onClose = () => reject(new Error(t("verifyCanceled")));
    const cancel = button(t("cancel"), () => done(new Error(t("verifyCanceled"))));
    const go = button(t("verifyGo"), async () => {
      go.disabled = true;
      msg.textContent = "";
      try {
        await signIn();
      } catch (e) {
        msg.textContent = passkeyError(e);
        go.disabled = false;
        return;
      }
      done();
    }, { cls: "primary" });
    msg = authDialog(t("verifyTitle"), t("verifyText"), cancel, go);
    d.addEventListener("close", onClose, { once: true });
  });
}

// openPasskeys shows the passkeys, to add or delete them, and to end the
// other sessions.
async function openPasskeys() {
  const d = $("passkeys");
  const head = el("div", "files-head", el("h2", "", t("passkeysTitle")), button("×", () => d.close(), { title: t("closeTitle") }));
  const msg = el("p", "error");
  let list;
  try {
    list = await api("/auth/passkeys");
  } catch (e) {
    d.replaceChildren(head, el("p", "error", t("authFailed", { error: e.message })));
    if (!d.open) d.showModal();
    return;
  }
  const run = async (f) => {
    msg.textContent = "";
    try {
      await f();
    } catch (e) {
      msg.textContent = e.name === "InvalidStateError" ? t("passkeyAlreadyHere") : e.name === "NotAllowedError" ? t("authCanceled") : e.message;
      return;
    }
    openPasskeys();
  };
  const rows = list.map((k, i) => el("li", "",
    el("span", "files-name", t("passkeyName", { n: i + 1 })), " ",
    el("span", "msg", t("passkeyCreated", { time: k.created_at ? formatTime(k.created_at) : "—" }) + (k.used_at ? " · " + t("passkeyUsed", { time: formatTime(k.used_at) }) : "")), " ",
    (() => {
      const b = button(t("passkeyDelete"), () => {
        if (confirm(t("confirmDeletePasskey", { n: i + 1 }))) run(() => api(`/auth/passkeys/${encodeURIComponent(k.id)}`, { method: "DELETE" }));
      });
      // The only passkey stays; ebira auth reset removes every passkey.
      if (list.length === 1) {
        b.disabled = true;
        b.title = t("passkeyLast");
      }
      return b;
    })()));
  const add = button(t("passkeyAdd"), () => run(async () => {
    if (!passkeysSupported()) throw new Error(t("authUnsupported"));
    const c = await api("/auth/passkey/register/begin", { method: "POST", body: {} });
    const cred = await navigator.credentials.create({ publicKey: PublicKeyCredential.parseCreationOptionsFromJSON(c.options) });
    await api("/auth/passkey/register/finish", { method: "POST", body: { ceremony: c.ceremony, credential: cred.toJSON() } });
  }));
  const revoke = button(t("revokeSessions"), () => {
    if (confirm(t("confirmRevokeSessions"))) run(() => api("/auth/sessions/revoke", { method: "POST" }));
  });
  d.replaceChildren(head, el("ul", "files-list", ...rows), msg,
    el("p", "msg", t("revokeSessionsText")),
    el("div", "files-actions", revoke, add));
  if (!d.open) d.showModal();
}

$("passkeys-open").addEventListener("click", () => openPasskeys());

// showDeviceApprove asks to approve the sign-in of a client without a
// browser, such as the TUI, which shows the same code and the URL of this
// page with it (#device/<code>). The page must be signed in with a passkey.
function showDeviceApprove(code) {
  const d = $("auth");
  delete d.dataset.required;
  let msg;
  const cancel = button(t("cancel"), () => d.close());
  const go = button(t("deviceApprove"), async () => {
    go.disabled = true;
    msg.textContent = "";
    try {
      await api("/auth/device/approve", { method: "POST", body: { user_code: code } });
    } catch (e) {
      msg.textContent = e.status === 404 ? t("deviceNotFound") : t("authFailed", { error: e.message });
      return;
    }
    authDialog(t("deviceTitle"), t("deviceApproved"), button(t("authDone"), () => d.close(), { cls: "primary" }));
  }, { cls: "primary" });
  msg = authDialog(t("deviceTitle"), el("div", "",
    el("p", "", t("deviceText")),
    el("div", "auth-cmd device-code", el("code", "", code))), cancel, go);
}

// passkeyError is the message of a failed ceremony; the browser tells a
// canceled one with NotAllowedError.
function passkeyError(e) {
  if (e.name === "NotAllowedError") return t("authCanceled");
  return t("authFailed", { error: e.message });
}

// openDeviceHash asks to approve the code of #device/<code>, the URL a
// client without a browser shows.
function openDeviceHash() {
  const device = location.hash.match(/^#device\/([A-Za-z-]+)$/);
  if (!device) return;
  history.replaceState(null, "", location.pathname + location.search);
  if (authState.signed_in) showDeviceApprove(device[1]);
}

// The URLs of ebira auth passkey and of a client may also be opened in the
// page already open, which changes only the hash.
window.addEventListener("hashchange", () => {
  const reg = location.hash.match(/^#register\/([\w-]+)$/);
  if (reg) {
    history.replaceState(null, "", location.pathname + location.search);
    showRegister(reg[1]);
    return;
  }
  openDeviceHash();
});

$("logout").addEventListener("click", async () => {
  try {
    await api("/auth/logout", { method: "POST" });
  } catch (_) { /* reloaded anyway */ }
  location.reload();
});

// Start

(async () => {
  // The note of the URL, read before the preview shows anything else, which
  // changes the address bar.
  const linked = noteLink(location.href);
  await loadMessages();
  try {
    authState = await api("/auth");
  } catch (_) { /* an older server: no passkeys */ }
  showAuthState();
  const reg = location.hash.match(/^#register\/([\w-]+)$/);
  if (reg) {
    history.replaceState(null, "", location.pathname + location.search);
    showRegister(reg[1]);
    return;
  }
  if (authState.mode === "passkey" && !authState.signed_in) {
    showSignIn(); // signing in reloads the page, with the hash of #device/<code>
    return;
  }
  openDeviceHash();
  for (const e of document.querySelectorAll(".kind-icon[data-kind]")) e.append(kindSVG(e.dataset.kind));
  for (const b of document.querySelectorAll("#side-tabs button")) {
    b.addEventListener("click", () => setSide(b.dataset.side));
  }
  renderNotebooks();
  try {
    await loadNotebooks();
  } catch (e) {
    $("list-info").textContent = t("serverUnreachable", { error: e.message });
    $("list-info").classList.add("error");
  }
  const selected = restoreView();
  renderNotebooks();
  await loadNotes();
  // The note shown before the reload, while it is still in the list.
  if (selected && listedNotes().some((n) => n.id === selected)) {
    state.selected = selected;
    markSelected();
    loadNote();
  }
  if (linked) selectNote(linked);
  connect();
  showGitSync();
})();
