// ebira web UI: notebooks, notes, search, a preview, an editor and questions
// to the assistant, over the REST API of the same server. Text from notes is put into the page with
// textContent; only the rendered Markdown body is HTML, sanitized with
// DOMPurify.
"use strict";

const state = {
  notebooks: [],
  notebook: null, // selected notebook ID; null for all but the trash
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
// the value of name.

let messages = {};

function t(key, vars) {
  const s = messages[key] ?? key;
  return vars ? s.replace(/\{(\w+)\}/g, (m, k) => (k in vars ? String(vars[k]) : m)) : s;
}

async function loadMessages() {
  try {
    const res = await fetch("i18n/ja.json");
    if (res.ok) messages = await res.json();
  } catch (_) { /* the keys are shown instead */ }
  // The page marks its text with data-i18n (the text), data-i18n-title,
  // data-i18n-placeholder and data-i18n-aria-label.
  for (const e of document.querySelectorAll("[data-i18n]")) e.textContent = t(e.dataset.i18n);
  for (const [attr, name] of [["title", "i18nTitle"], ["placeholder", "i18nPlaceholder"], ["aria-label", "i18nAriaLabel"]]) {
    for (const e of document.querySelectorAll(`[data-${attr === "aria-label" ? "i18n-aria-label" : "i18n-" + attr}]`)) e.setAttribute(attr, t(e.dataset[name]));
  }
}

// API

async function api(path, { method = "GET", params, body, ifMatch, signal } = {}) {
  const url = new URL(path, location.origin);
  for (const [k, v] of Object.entries(params || {})) {
    if (v !== null && v !== undefined && v !== "") url.searchParams.set(k, v);
  }
  const headers = { Accept: "application/json" };
  if (body !== undefined) headers["Content-Type"] = "application/json";
  if (ifMatch) headers["If-Match"] = ifMatch;
  const res = await fetch(url, { method, headers, body: body === undefined ? undefined : JSON.stringify(body), signal });
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

DOMPurify.addHook("afterSanitizeAttributes", (node) => {
  // Links to notes open in this page; the others in a new tab.
  if (node.tagName === "A" && node.hasAttribute("href") && !noteLink(node.getAttribute("href"))) {
    node.setAttribute("target", "_blank");
    node.setAttribute("rel", "noopener noreferrer");
  }
});

// renderBody renders the Markdown body into el, with the ranges highlighted.
function renderBody(el, body, ranges) {
  const html = marked.parse(markSource(body, ranges), { gfm: true });
  el.innerHTML = DOMPurify.sanitize(html);
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
  return d.toLocaleString("ja-JP", { year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });
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

function tagList(tags) {
  return (tags || []).map((tag) => el("span", "tag", tag));
}

// Notebooks

async function loadNotebooks() {
  state.notebooks = await api("/notebooks");
  if (state.notebook && !state.notebooks.some((n) => n.id === state.notebook)) {
    state.notebook = null; // deleted
  }
  renderNotebooks();
}

function renderNotebooks() {
  const ul = $("notebooks");
  ul.replaceChildren();
  const total = state.notebooks.filter((n) => n.id !== "trash").reduce((a, n) => a + n.count, 0);
  const item = (id, name, icon, count, cls) => {
    const li = el("li", cls || "", el("span", "name", name));
    if (icon) li.append(el("span", "icon", icon));
    li.append(el("span", "count", String(count)));
    if (state.notebook === id) li.classList.add("active");
    li.addEventListener("click", () => selectNotebook(id));
    return li;
  };
  ul.append(item(null, t("all"), "", total));
  for (const nb of state.notebooks) {
    const li = item(nb.id, nb.name, kindIcon(nb.kind), nb.count, nb.id === "trash" || nb.id === "inbox" ? "sep" : "");
    if (!nb.reserved) enableNotebookDrag(li, nb.id);
    ul.append(li);
  }
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
  state.notebook = id;
  renderNotebooks();
  loadNotes({ chosen: true });
}

// Notes

let loadSeq = 0; // drops responses of requests overtaken by later ones

// checkSelection is set when the user changes the list, by choosing a notebook
// or a search query. The note in the preview then stays only if the new list
// has it. Reloads on change events and after saving do not clear it, so that a
// note just moved elsewhere stays in view.
let checkSelection = false;

async function loadNotes({ chosen = false } = {}) {
  if (chosen) {
    checkSelection = true;
    state.want = LIST_PAGE; // a new list starts with one page
  }
  state.want ||= LIST_PAGE;
  const seq = ++loadSeq;
  const info = $("list-info");
  try {
    let notes, total, mode;
    if (state.query) {
      const r = await api("/search", { params: { q: state.query, notebook: state.notebook, limit: 200 } });
      notes = r.notes;
      total = r.total;
      mode = r.mode;
    } else {
      // A notebook's list comes in its own sort, which the server knows.
      // Pages are read until the notes shown so far, so that a reload keeps
      // the pages read with "load more".
      notes = [];
      do {
        const r = await api("/notes", { params: { notebook: state.notebook, sort: state.notebook ? null : state.allSort, limit: LIST_PAGE, offset: notes.length } });
        notes.push(...r.notes);
        total = r.total;
        if (!r.notes.length) break;
      } while (notes.length < Math.min(state.want, total));
    }
    if (seq !== loadSeq) return;
    state.notes = notes;
    state.total = total;
    if (mode) $("mode").textContent = mode;
    const where = state.notebook ? notebookName(state.notebook) : t("all");
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
    ...tagList(n.tags));
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

function startEdit(note) {
  if (state.editing) return;
  if (note && note.notebook === "trash") return;
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
  f.tags.value = note ? note.tags.join(", ") : "";
  f.body = el("textarea", "editor-body");
  f.body.placeholder = t("bodyPlaceholder");
  f.body.spellcheck = false;
  f.body.value = note ? note.body : "";
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
  (note ? f.body : f.title).focus();
}

function formValues() {
  const f = state.editing.elems;
  return {
    title: f.title.value.trim(),
    notebook: f.notebook.value,
    tags: parseTags(f.tags.value),
    body: f.body.value,
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
  state.editing = null;
  return true;
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
  if (ed.preview) renderBody(f.rendered, f.body.value, null);
  f.body.hidden = ed.preview;
  f.rendered.hidden = !ed.preview;
  f.toggle.textContent = t(ed.preview ? "editBody" : "preview");
  if (!ed.preview) f.body.focus();
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
  state.editing = null;
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
      state.editing = null;
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
  if ($("conflict").open) return;
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
