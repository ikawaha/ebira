// The parts of CodeMirror 6 that the web UI uses, bundled by build.mjs into
// static/vendor/codemirror.js as the global CM. The editor itself is set up
// in static/app.js, so that changing it needs no build.

import { LanguageDescription, StreamLanguage } from "@codemirror/language";
import { css } from "@codemirror/lang-css";
import { go } from "@codemirror/lang-go";
import { html } from "@codemirror/lang-html";
import { javascript } from "@codemirror/lang-javascript";
import { json } from "@codemirror/lang-json";
import { python } from "@codemirror/lang-python";
import { sql } from "@codemirror/lang-sql";
import { yaml } from "@codemirror/lang-yaml";
import { shell } from "@codemirror/legacy-modes/mode/shell";

export { EditorView, keymap, placeholder } from "@codemirror/view";
export { EditorState, EditorSelection } from "@codemirror/state";
export { defaultKeymap, history, historyField, historyKeymap, indentWithTab } from "@codemirror/commands";
export { indentUnit, syntaxHighlighting } from "@codemirror/language";
export { markdown, markdownLanguage } from "@codemirror/lang-markdown";
export { classHighlighter, tagHighlighter, tags } from "@lezer/highlight";

// lang returns the description of a language of code blocks, named by the
// info string of a fence.
function lang(name, alias, support) {
  return LanguageDescription.of({ name, alias, load: () => Promise.resolve(support()) });
}

// codeLanguages are the languages colored in code blocks. Add one here, with
// its package in package.json, and build again.
export const codeLanguages = [
  lang("CSS", ["css"], css),
  lang("Go", ["go", "golang"], go),
  lang("HTML", ["html", "htm"], html),
  lang("JavaScript", ["javascript", "js", "mjs", "cjs"], javascript),
  lang("TypeScript", ["typescript", "ts"], () => javascript({ typescript: true })),
  lang("JSX", ["jsx"], () => javascript({ jsx: true })),
  lang("TSX", ["tsx"], () => javascript({ jsx: true, typescript: true })),
  lang("JSON", ["json"], json),
  lang("Python", ["python", "py"], python),
  lang("SQL", ["sql"], sql),
  lang("YAML", ["yaml", "yml"], yaml),
  lang("Shell", ["sh", "bash", "shell", "zsh", "console"], () => StreamLanguage.define(shell)),
];
