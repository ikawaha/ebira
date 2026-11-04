---
title: Syncing
tags: [guide, sync]
---
Notes are plain files, so any way of syncing files works.

## Dropbox, iCloud Drive and others

Just keep the notes directory in the synced folder. When a note is changed on two machines at once and the service leaves a conflicted copy, ebira makes it a note titled with `(conflict copy)`. Neither is lost.

## git

Make the notes directory a git repository, and ebira syncs it.

- "Sync with git" in the gear menu, or `ebira sync`, commits, fetches, merges and pushes.
- `git_sync: 10m` in the settings file syncs on its own.
- A note changed on both sides keeps yours, and the other becomes a new note titled with `(conflict …)`.

A git repository in a synced folder can break, so use one or the other.
