// theme.js sets the color theme before the page is drawn, so that the page
// does not flash the other one first. It runs
// alone in the head; app.js changes the theme later.
//
// The html element's data-theme is light or dark, or absent to follow the
// system: the server puts its theme there (serve -theme, theme of the settings
// file or of its defaults), as its lang attribute. Both are kept, as
// data-server-theme and data-server-lang, for the settings dialog; app.js
// sets lang to the language it shows.
(() => {
  const root = document.documentElement;
  root.dataset.serverTheme = root.dataset.theme || "auto";
  root.dataset.serverLang = root.getAttribute("lang") || "";
  // Earlier versions kept a language and a theme chosen in the browser.
  try {
    localStorage.removeItem("ebira.lang");
    localStorage.removeItem("ebira.theme");
  } catch (_) { /* none */ }
  // The size of the text of the body of notes, also chosen in the menu, as a
  // percentage in ebira.bodySize.
  let size = NaN;
  try { size = Number(localStorage.getItem("ebira.bodySize")); } catch (_) { /* none */ }
  if (size >= 50 && size <= 300) root.style.setProperty("--body-scale", String(size / 100));
})();
