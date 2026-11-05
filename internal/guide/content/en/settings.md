---
title: Settings
tags: [guide, settings]
---
## The gear menu

- Language and theme (auto, light or dark)
- Text size of notes (− and +)
- Style…: how the preview looks (Band, Document, Compact or Lined)
- Names of the marks…
- Settings file…: edit the settings file below
- Import Markdown (or drop files onto the page)
- Check images, Server status, Sync with git
- Add the guide
- Report an issue: open the page to post a request or a bug on GitHub
- At the bottom, the version of ebira; choose it to copy the details, to tell in a report

## The settings file

What you write in `~/.config/ebira/config.yaml` becomes the default of the commands.

```yaml
notes_dir: ~/notes   # the notes directory
addr: localhost:13579
lang: en             # language of the UI
theme: auto
git_sync: 10m        # how often to sync with git
auth: none           # passkey asks for a passkey
```

You can edit it with Settings file… in the gear menu too. The server reads it when it starts, so Save and restart applies it (`notes_dir` and `addr` change when the server is stopped and started).

The settings synced with the notes are in `_ebira.yaml` of the notes directory, such as the search mode `search`.

## CSS of the preview

Put `preview.css` next to the settings file to style the preview with your own CSS.
