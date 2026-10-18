// ebira web UI: notebooks, notes, search, a preview, an editor and questions
// to the assistant, over the REST API of the same server. Text from notes is put into the page with
// textContent; only the rendered Markdown body is HTML, sanitized with
// DOMPurify.
"use strict";

const state = {
  notebooks: [],
  notebook: null, // selected notebook ID; null for all but the trash
  tags: [], // tags with their note counts
  tag: null, // selected tag; the list then has the notes with it in all notebooks
  side: loadSide(), // what the left column lists: "notebooks" or "tags"
  query: "",
  notes: [],
  total: 0,
  selected: null, // ID of the note in the preview
  note: null, // the note in the preview
  editing: null, // the editor's state while editing; see startEdit
  allSort: loadAllSort(), // sort of all notes; a notebook keeps its own on the server
  want: 0, // number of notes of the list to show, a page more on "load more"
};

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

const $ = (id) => document.getElementById(id);

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
    const res = await fetch(`i18n/${l}.json`);
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
}

// API

// api calls the REST API. body is sent as JSON; raw, such as a File, as is.
async function api(path, { method = "GET", params, body, raw, ifMatch, signal } = {}) {
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
    try {
      const e = await res.json();
      if (e.message) msg = e.message;
    } catch (_) { /* not JSON */ }
    const err = new Error(msg);
    err.status = res.status;
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

// noteLink returns the note ID of a link to a note in this UI (/ui/#note/<ID>),
// which the assistant puts in its answers, or null.
function noteLink(href) {
  let u;
  try {
    u = new URL(href, location.href);
  } catch (_) {
    return null;
  }
  if (u.origin !== location.origin || u.pathname !== "/ui/") return null;
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
    await loadScript("vendor/katex/katex.min.js", "vendor/katex/katex.min.css");
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

async function drawDiagrams(el) {
  const nodes = [...el.querySelectorAll(".mermaid[data-src]")].filter((n) => n.isConnected && !n.firstChild);
  if (!nodes.length) return;
  try {
    mermaidReady ||= loadScript("vendor/mermaid.min.js").then(() => mermaid.initialize({
      startOnLoad: false,
      securityLevel: "strict",
      htmlLabels: false,
      theme: matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "default",
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

const kindIcon = (kind) => ({ collab: "🤖", drop: "📥", private: "🔒" })[kind] || "";

// aiMark returns the mark of a note created by an AI client, which goes at the
// end of a row rather than right after the title.
function aiMark(note, label) {
  if (note.created_by !== "ai") return null;
  const m = el("span", "ai-mark", label ? `🤖 ${label}` : "🤖");
  m.title = t("aiNote");
  return m;
}

function notebookName(id) {
  const nb = state.notebooks.find((n) => n.id === id);
  return nb ? nb.name : id;
}

function formatTime(s) {
  const d = new Date(s);
  if (isNaN(d)) return s;
  return d.toLocaleString(lang, { year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });
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
  const [notebooks, tags] = await Promise.all([api("/notebooks"), api("/tags")]);
  state.notebooks = notebooks;
  state.tags = tags;
  if (state.notebook && !state.notebooks.some((n) => n.id === state.notebook)) {
    state.notebook = null; // deleted
  }
  renderNotebooks();
}

// renderNotebooks renders the left column: the notebooks or the tags, each
// below "all notes".
function renderNotebooks() {
  for (const b of document.querySelectorAll("#side-tabs button")) {
    b.setAttribute("aria-selected", String(b.dataset.side === state.side));
  }
  const ul = $("notebooks");
  ul.replaceChildren();
  const total = state.notebooks.filter((n) => n.id !== "trash").reduce((a, n) => a + n.count, 0);
  const all = el("li", !state.tag && !state.notebook ? "active" : "", el("span", "name", t("all")), el("span", "count", String(total)));
  all.addEventListener("click", () => selectNotebook(null));
  ul.append(all);
  if (state.side === "tags") {
    renderTags(ul);
    return;
  }
  const item = (id, name, icon, count, cls) => {
    const li = el("li", cls || "", el("span", "name", name));
    if (icon) li.append(el("span", "icon", icon));
    li.append(el("span", "count", String(count)));
    if (!state.tag && state.notebook === id) li.classList.add("active");
    li.addEventListener("click", () => selectNotebook(id));
    li.dataset.notebook = id;
    acceptImport(li, id);
    return li;
  };
  for (const nb of state.notebooks) {
    const li = item(nb.id, nb.name, kindIcon(nb.kind), nb.count, nb.id === "trash" || nb.id === "inbox" ? "sep" : "");
    if (!nb.reserved) enableNotebookDrag(li, nb.id);
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

// notebookDrop returns the ID of the notebook to put the dragged one before
// (null for last) and whether the drop goes below li.
function notebookDrop(li, e) {
  const r = li.getBoundingClientRect();
  const below = e.clientY > r.top + r.height / 2;
  const others = state.notebooks.filter((n) => !n.reserved && n.id !== nbDragID);
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
  li.addEventListener("dragend", () => {
    nbDragID = null;
    li.classList.remove("dragging");
    for (const x of $("notebooks").children) x.classList.remove("drop-above", "drop-below");
  });
  li.addEventListener("dragover", (e) => {
    if (!nbDragID || nbDragID === id) return;
    e.preventDefault();
    const { below } = notebookDrop(li, e);
    li.classList.toggle("drop-below", below);
    li.classList.toggle("drop-above", !below);
  });
  li.addEventListener("dragleave", () => li.classList.remove("drop-above", "drop-below"));
  li.addEventListener("drop", (e) => {
    if (!nbDragID || nbDragID === id) return;
    e.preventDefault();
    const moved = nbDragID;
    const { before } = notebookDrop(li, e);
    moveNotebook(moved, before);
  });
}

async function moveNotebook(id, before) {
  const moved = state.notebooks.find((n) => n.id === id);
  const others = state.notebooks.filter((n) => !n.reserved && n.id !== id);
  others.splice(before ? others.findIndex((n) => n.id === before) : others.length, 0, moved);
  const changed = [];
  others.forEach((nb, k) => {
    const order = (k + 1) * 10;
    if (nb.order !== order) {
      nb.order = order;
      changed.push({ id: nb.id, order });
    }
  });
  // Show the new order at once; inbox stays first and trash last.
  state.notebooks = [
    ...state.notebooks.filter((n) => n.reserved && n.id !== "trash"),
    ...others,
    ...state.notebooks.filter((n) => n.id === "trash"),
  ];
  renderNotebooks();
  notebookSaves = notebookSaves.then(async () => {
    try {
      for (const c of changed) {
        await api(`/notebooks/${encodeURIComponent(c.id)}`, { method: "PUT", body: { order: c.order } });
      }
    } catch (e) {
      alert(t("notebookOrderNotSaved", { error: e.message }));
    }
  });
  await notebookSaves;
  loadNotebooks().catch(() => {});
}

function selectNotebook(id) {
  if (!state.tag && id === state.notebook) return;
  state.tag = null;
  state.notebook = id;
  renderNotebooks();
  loadNotes({ chosen: true, first: true });
}

// selectTag lists the notes with tag in all notebooks but the trash, as
// choosing a notebook does.
function selectTag(tag) {
  if (state.side !== "tags") setSide("tags"); // show the tag chosen in the preview
  if (tag === state.tag) return;
  state.tag = tag;
  state.notebook = null;
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
    checkSelection = true;
    pickFirst = first;
    state.want = LIST_PAGE; // a new list starts with one page
  }
  state.want ||= LIST_PAGE;
  const seq = ++loadSeq;
  const info = $("list-info");
  try {
    let notes, total, mode;
    if (state.query) {
      const r = await api("/search", { params: { q: state.query, notebook: state.notebook, tag: state.tag, limit: 200 } });
      notes = r.notes;
      total = r.total;
      mode = r.mode;
    } else {
      // A notebook's list comes in its own sort, which the server knows.
      // Pages are read until the notes shown so far, so that a reload keeps
      // the pages read with "load more".
      notes = [];
      do {
        const r = await api("/notes", { params: { notebook: state.notebook, tag: state.tag, sort: state.notebook ? null : state.allSort, limit: LIST_PAGE, offset: notes.length } });
        notes.push(...r.notes);
        total = r.total;
        if (!r.notes.length) break;
      } while (notes.length < Math.min(state.want, total));
    }
    if (seq !== loadSeq) return;
    state.notes = notes;
    state.total = total;
    if (mode) $("mode").textContent = mode;
    const where = state.tag ? "#" + state.tag : state.notebook ? notebookName(state.notebook) : t("all");
    info.textContent = state.query
      ? t(total > notes.length ? "searchInfoTop" : "searchInfo", { where, query: state.query, total, shown: notes.length })
      : t(total > notes.length ? "listInfoPart" : "listInfo", { where, total, shown: notes.length });
    info.classList.remove("error");
    renderSort();
    renderNotes();
    applySelectionCheck();
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
    const first = state.notes[0]?.id ?? null;
    if (first !== state.selected) {
      state.selected = first;
      markSelected();
      loadNote();
      return;
    }
  }
  if (state.selected && !state.notes.some((n) => n.id === state.selected)) {
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
      await api(`/notebooks/${encodeURIComponent(id)}`, { method: "PUT", body: { sort } });
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
async function moveNote(id, after) {
  try {
    await api(`${noteURL(id)}/move`, { method: "POST", body: after ? { after } : {} });
  } catch (e) {
    alert(t("moveFailed", { error: e.message }));
  }
  await loadNotes();
  markSelected();
}

// moveSelected moves the selected note up (-1) or down (+1).
function moveSelected(delta) {
  if (!canReorder()) return;
  const i = state.notes.findIndex((n) => n.id === state.selected);
  const j = i + delta;
  if (i < 0 || j < 0 || j >= state.notes.length) return;
  // Down: after the next note. Up: after the note above the previous one, or
  // first.
  const after = delta > 0 ? state.notes[j].id : (state.notes[j - 1]?.id ?? null);
  moveNote(state.selected, after);
}

// Drag and drop in the manual order: dropping on the upper half of a note puts
// the dragged note above it, on the lower half below it.
let dragID = null;

function dropAfter(li, e) {
  const r = li.getBoundingClientRect();
  const below = e.clientY > r.top + r.height / 2;
  const others = state.notes.filter((n) => n.id !== dragID);
  const i = others.findIndex((n) => n.id === li.dataset.id);
  if (below) return { after: others[i].id, below };
  return { after: i > 0 ? others[i - 1].id : null, below };
}

function enableDrag(li) {
  li.draggable = true;
  li.addEventListener("dragstart", (e) => {
    dragID = li.dataset.id;
    e.dataTransfer.effectAllowed = "move";
    li.classList.add("dragging");
  });
  li.addEventListener("dragend", () => {
    dragID = null;
    li.classList.remove("dragging");
    for (const x of $("notes").children) x.classList.remove("drop-above", "drop-below");
  });
  li.addEventListener("dragover", (e) => {
    if (!dragID || dragID === li.dataset.id) return;
    e.preventDefault();
    const { below } = dropAfter(li, e);
    li.classList.toggle("drop-below", below);
    li.classList.toggle("drop-above", !below);
  });
  li.addEventListener("dragleave", () => li.classList.remove("drop-above", "drop-below"));
  li.addEventListener("drop", (e) => {
    if (!dragID || dragID === li.dataset.id) return;
    e.preventDefault();
    const id = dragID;
    const { after } = dropAfter(li, e);
    moveNote(id, after);
  });
}

function renderNotes() {
  const ul = $("notes");
  ul.replaceChildren();
  const sort = currentSort();
  const reorder = canReorder();
  for (const n of state.notes) {
    const li = el("li", n.id === state.selected ? "active" : "");
    li.dataset.id = n.id;
    if (reorder) enableDrag(li);
    const head = el("div", "title-row", el("div", "title", highlighted(n.title || t("untitled"), n.title_highlights)));
    const mark = aiMark(n);
    if (mark) head.append(mark);
    li.append(head);
    // The time shown follows the sort: creation for the creation order (and
    // the manual order, which starts as it), update otherwise.
    const created = !state.query && (sort === "created" || sort === "order");
    const time = created ? t("createdAt", { time: formatTime(n.created_at) }) : formatTime(n.updated_at);
    const meta = el("div", "meta", el("span", "", time));
    if (!state.notebook) meta.append(el("span", "", notebookName(n.notebook)));
    meta.append(...tagList(n.tags));
    li.append(meta);
    if (n.snippet) li.append(el("div", "snippet", highlighted(n.snippet, n.snippet_highlights)));
    li.addEventListener("click", () => selectNote(n.id));
    ul.append(li);
  }
  if (!state.query && state.notes.length < state.total) {
    const more = button(t("loadMore", { rest: state.total - state.notes.length }), () => {
      state.want = state.notes.length + LIST_PAGE;
      loadNotes();
    }, { cls: "more" });
    ul.append(el("li", "more-row", more));
  }
}

function markSelected() {
  for (const li of $("notes").children) li.classList.toggle("active", li.dataset.id === state.selected);
  $("notes").querySelector("li.active")?.scrollIntoView({ block: "nearest" });
}

function selectNote(id) {
  if (id === state.selected && !state.editing) return;
  if (!leaveEditor()) return;
  state.selected = id;
  markSelected();
  loadNote();
}

function moveSelection(delta) {
  if (!state.notes.length) return;
  const i = state.notes.findIndex((n) => n.id === state.selected);
  const next = i < 0 ? 0 : Math.min(Math.max(i + delta, 0), state.notes.length - 1);
  selectNote(state.notes[next].id);
}

// Preview

let noteSeq = 0;
let marks = [];
let markIndex = -1;

async function loadNote() {
  if (state.editing) return; // the editor keeps its own copy
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
    el("span", "", notebookName(n.notebook) + (n.trashed_from ? t("trashedFrom", { notebook: notebookName(n.trashed_from) }) : "")),
    el("span", "", t("updatedAt", { time: formatTime(n.updated_at) })),
    ...tagList(n.tags, true));
  const mark = aiMark(n, t("createdByAI"));
  if (mark) meta.append(mark);
  const actions = el("div", "actions");
  if (n.notebook === "trash") {
    actions.append(
      button(t("restore"), restoreNote, { title: t("restoreTitle") }),
      button(t("deletePermanently"), deleteNote, { cls: "danger" }));
  } else {
    actions.append(
      button(t("edit"), () => startEdit(n), { title: t("editTitle") }),
      button(t("toTrash"), deleteNote));
  }
  const body = el("div", "body");
  renderBody(body, n.body || "", n.body_highlights);
  showPreview(el("header", "note-head",
    el("div", "head-row", el("h2", "", highlighted(n.title || t("untitled"), n.title_highlights)), actions),
    meta), body);
  pv.dataset.id = id;
  marks = [...pv.querySelectorAll("mark")];
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
}

function showPreview(...children) {
  const pv = $("preview");
  pv.classList.remove("editing");
  pv.replaceChildren(...children);
  delete pv.dataset.id;
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
  const i = state.notes.findIndex((x) => x.id === n.id);
  const next = state.notes[i + 1] || state.notes[i - 1];
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
  editorReady ||= loadScript("vendor/codemirror.js").then(() => {
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
    CM.keymap.of([...CM.defaultKeymap, ...CM.historyKeymap, CM.indentWithTab]),
    CM.syntaxHighlighting(CM.classHighlighter),
    CM.syntaxHighlighting(markup),
    CM.EditorView.lineWrapping,
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
    const o = el("option", "", nb.name + (kindIcon(nb.kind) ? " " + kindIcon(nb.kind) : ""));
    o.value = nb.id;
    f.notebook.append(o);
  }
  f.notebook.value = note ? note.notebook : defaultNotebook;
  f.tags = el("input");
  f.tags.placeholder = t("tagsPlaceholder");
  f.tags.value = note ? note.tags.join(", ") : state.tag || ""; // a new note under a tag gets it
  f.body = el("div", "editor-body");
  ed.view = new CM.EditorView({ parent: f.body, doc: note ? note.body : "", extensions: bodyExtensions() });
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

  const pv = $("preview");
  pv.replaceChildren(form);
  pv.classList.add("editing");
  delete pv.dataset.id;
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

// endEdit drops the editor, which the caller replaces.
function endEdit() {
  state.editing?.view.destroy();
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
  a.href = `/ui/#note/${encodeURIComponent(id)}`;
  a.addEventListener("click", () => d.close());
  return a;
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
  if (open) {
    renderAskEmpty();
    $("ask-q").focus();
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
    $("ask-send").disabled = false;
  }
}

$("ask-toggle").addEventListener("click", () => toggleAssistant());
// Settings menu: the gear at the bottom of the left column.

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
});
$("ask-form").addEventListener("submit", (e) => {
  e.preventDefault();
  const q = $("ask-q").value.trim();
  if (!q || askController) return;
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

// Links to notes (/ui/#note/<ID>), in answers or in notes, open the note in the
// preview. The same URL opened from elsewhere does too.
document.addEventListener("click", (e) => {
  const a = e.target.closest?.("a[href]");
  if (!a || e.metaKey || e.ctrlKey || e.shiftKey) return;
  const id = noteLink(a.getAttribute("href"));
  if (!id) return;
  e.preventDefault();
  selectNote(id);
});

function openLinkedNote() {
  const id = noteLink(location.href);
  if (!id) return;
  history.replaceState(null, "", location.pathname + location.search);
  selectNote(id);
}

window.addEventListener("hashchange", openLinkedNote);

// Search

let searchTimer;

$("q").addEventListener("input", () => {
  clearTimeout(searchTimer);
  searchTimer = setTimeout(() => {
    const q = $("q").value.trim();
    if (q === state.query) return;
    state.query = q;
    loadNotes({ chosen: true }).then(() => loadNote()); // refresh the highlights
  }, 250);
});

$("search-form").addEventListener("submit", (e) => e.preventDefault());

// Keys: / search, Esc clear, j/k or arrows move, n/N jump between matches,
// e edit, c create, u restore, s sort, J/K move the note in the manual order,
// a assistant; in the editor, ⌘S / Ctrl+S save and Esc cancel; in the
// assistant, Esc leaves the question box and then closes it.

document.addEventListener("keydown", (e) => {
  if ($("conflict").open || $("files-check").open || $("import-result").open) return;
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
    case "n": jumpMark(1); break;
    case "N": jumpMark(-1); break;
    case "J": e.preventDefault(); moveSelected(1); break;
    case "K": e.preventDefault(); moveSelected(-1); break;
    case "s": e.preventDefault(); if (!state.query) cycleSort(); break;
    case "e": if (state.note) { e.preventDefault(); startEdit(state.note); } break;
    case "c": e.preventDefault(); startEdit(null); break;
    case "u": restoreNote(); break;
    case "a": e.preventDefault(); toggleAssistant(true); break;
    case "T": e.preventDefault(); setSide(state.side === "tags" ? "notebooks" : "tags"); break;
    case "Escape": if (!$("assistant").hidden) toggleAssistant(false); break;
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
  es.addEventListener("open", () => { status.className = "status ok"; status.title = t("eventsConnected"); });
  es.addEventListener("error", () => { status.className = "status ng"; status.title = t("eventsReconnecting"); });
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

// Start

(async () => {
  await loadMessages();
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
  await loadNotes();
  openLinkedNote();
  connect();
})();
