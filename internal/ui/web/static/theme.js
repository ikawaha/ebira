// theme.js sets the color theme before the page is drawn, so that a theme
// chosen in the settings menu does not flash the other one first. It runs
// alone in the head; app.js changes the theme later.
//
// The html element's data-theme is light or dark, or absent to follow the
// system. The server puts its theme there (serve -theme or theme of the
// settings file); a theme chosen in the settings menu, kept in localStorage as
// ebira.theme (auto, light or dark), wins. The server's theme is kept in
// data-server-theme for the menu.
(() => {
  const root = document.documentElement;
  root.dataset.serverTheme = root.dataset.theme || "auto";
  let chosen = null;
  try { chosen = localStorage.getItem("ebira.theme"); } catch (_) { /* none */ }
  if (chosen === "light" || chosen === "dark") root.dataset.theme = chosen;
  else if (chosen === "auto") delete root.dataset.theme;
})();
