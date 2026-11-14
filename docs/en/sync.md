---
title: Syncing
tags: [guide, sync]
---
Notes are plain files, so any way of syncing files works. Only the notes are synced: the search index and the state of the server stay on each machine.

## Dropbox, iCloud Drive and others

Put the notes directory in a folder that a file sync service syncs, such as Dropbox, iCloud Drive, OneDrive, Google Drive or Syncthing. ebira needs no settings for it.

1. On the first machine, answer a directory in the synced folder when ebira sets up, such as `~/Dropbox/notes`. Setup tells you that the folder is synced, and does not offer git.
2. On the other machines, wait until the service has synced the folder, then answer the same directory. ebira finds the notes and uses their settings, such as the search mode, and builds an index of them on that machine. The path may differ between machines; each saves its own `notes_dir`.

Starting the second machine before the folder is synced makes new, empty notes, whose settings then clash with the first machine's.

When a note is changed on two machines, the service keeps one version and leaves the other as a copy, such as `3f9a… (MacBook's conflicted copy 2026-10-18).yaml` or `3f9a… 2.yaml`. ebira makes such a copy into a new note next to it, titled with `(conflict copy)`, or removes it when it is the same as the note. Neither change is lost.

- Keep the folder on the disk, so that searches and the index do not wait for downloads: *Make available offline* for Dropbox, *Keep Downloaded* for iCloud Drive, *Always keep on this device* for OneDrive. With selective sync, include the folder on every machine.
- Do not use git in a synced folder: the services can break a repository, and a change synced from another machine would be committed again. Use one or the other.

## git

git is optional. "Sync with git" in the gear menu shows only for notes in a repository. It, `ebira sync`, and `git_sync: 10m` in the settings file (which syncs on its own at that interval) all do the same, in the server:

1. Commit the changes of the notes directory, with the changed files in the message.
2. If the branch has an upstream branch, fetch it and merge it.
3. Push.

To start syncing notes you already have, connect them to a repository once, an empty one or one with notes:

```sh
ebira sync -remote git@github.com:you/notes.git
```

The repository is the notes directory itself, as set up by ebira, or one above it that tracks it. The remote and the branch are those of git: ebira syncs with the upstream branch of the current branch, so change them with `git remote set-url` and `git branch --set-upstream-to`. ebira runs your `git`, so your credentials, SSH keys and Git LFS settings apply; it never asks for a password, so set up a credential helper or an SSH agent for syncing in the background.

A merge never stops in conflict. A note changed on both machines keeps this machine's version, and the other version becomes a new note next to it, titled with `(conflict …)` and the time: compare them and delete the one you do not need. Another file changed on both sides keeps this side's version, and a file deleted on one side and changed on the other keeps the changed one; the merge commit has the other versions. If you merge by hand and leave conflict markers in a note, ebira reports the note and keeps showing its last good version.
