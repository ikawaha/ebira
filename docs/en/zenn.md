---
title: Writing to Zenn (beta)
tags: [guide, zenn]
---
A note can go to [Zenn](https://zenn.dev) as an article: ebira writes the article file and its images to the GitHub repository that Zenn deploys from, and pushes. Zenn takes the articles from there.

> **This is a beta.** How it writes and its settings may change.
>
> **It works only with Zenn connected to GitHub.** ebira writes to the GitHub repository, not to the web editor of Zenn. Unless a GitHub repository is connected on the dashboard of Zenn, the pushes put no articles on Zenn. Neither can ebira rewrite the articles you write in the web editor of Zenn.

## What it needs

ebira reaches neither Zenn nor GitHub itself: the server runs the `git` command to clone, commit and push (no API key of GitHub or Zenn is needed). So it needs these, all on the machine the ebira server runs on (not the one of the browser):

- **git**: a `git` the server finds. On macOS, the Command Line Tools of Xcode or git of Homebrew; on Windows, Git for Windows. A server started by the desktop app has a short PATH, so it takes git from a usual place such as `/usr/bin/git`. When the server finds no git, the dialog to write a note says so, and does not write (see *Getting git ready* below for how to install it).
- **Authentication to push to GitHub** that asks nothing: an SSH key (ssh-agent, or the SSH agent of 1Password) or a credential helper for HTTPS. ebira asks for no password, so when `git push` asks you in a terminal, notes cannot be written.
- **A name and an email for the commits**: `user.name` and `user.email` of git. When git signs commits, that has to go without typing too (signing with 1Password shows its approval each time a note is written).
- **The network**: GitHub reached from the machine of the server. Each git command is stopped after 2 minutes.
- **Zenn connected to GitHub**: a GitHub repository connected on the dashboard of Zenn (the GitHub app of Zenn reads it) and the branch Zenn deploys. Without it, this does not work (see Setting it up below).

### Getting git ready

On the machine the ebira server runs on, check these in a terminal, one by one:

1. **Is git there?** `git --version` shows its version when it is. If not, install it:
   - macOS: install the Command Line Tools of Xcode with `xcode-select --install` (`/usr/bin/git` then works). git of Homebrew (`brew install git`) alone may not be found by a server the desktop app started, so the Command Line Tools make sure of it.
   - Windows: install [Git for Windows](https://gitforwindows.org/), and start the desktop app again.
   - Linux: install it from the packages of the distribution (such as `sudo apt install git`).
2. **Who commits**: set `git config --global user.name "Your Name"` and `git config --global user.email "you@example.com"`.
3. **Authentication to push**: `git ls-remote git@github.com:you/zenn-content.git` (the URL of your repository) lists the branches without asking for a password or a passphrase.
   - With an SSH URL, register your public key on GitHub, and keep the key in ssh-agent (or the SSH agent of 1Password).
   - With an HTTPS URL, let a credential helper (Git Credential Manager, the macOS keychain or another) keep your GitHub credentials.

Then open the dialog again. When the dialog still finds no git after you installed it, restart the server (the desktop app).

## Setting it up

1. Connect a GitHub repository on the dashboard of Zenn, as Zenn tells in its guide to managing contents with a GitHub repository. The repository and the branch connected there are where ebira writes.
2. In *Settings…* of the gear menu, on the *Integrations* tab, write the URL of the repository under *Zenn (beta)* (such as `git@github.com:you/zenn-content.git`) and save. Write the branch when Zenn deploys another branch than the default one of the repository. With your user name on Zenn, the articles get their links. In the settings file, these are `zenn_repo`, `zenn_branch` and `zenn_user` ([Settings](settings.md)). They apply once saved.

ebira clones the repository for itself, into the state directory of this machine (`~/.local/state/ebira/…/zenn/`), and leaves the work trees you use alone. It runs git as you set it up, so your credentials, SSH keys and commit signing work as they do.

## Writing an article

The button of the letter Z with an arrow out to the top right, at the top right of the preview, opens *Write to Zenn*:

- **Emoji**: the eye-catch of the article, chosen at random: pick another with the arrows beside it, or type one.
- **Type**: Tech (a technical article) or Idea.
- **Topics**: up to 5, separated by commas. They start from the tags of the note, in lower case, without those with symbols or spaces (such as `C++`: write it as `cpp`).
- **Publish**: unchecked, the article is a draft (as it starts).

*Write* writes the note to `articles/<ID of the note>.md` and its images to `images/<ID of the note>/`, commits and pushes; Zenn shows it in a moment. The title is that of the note (Zenn takes up to 70 characters).

Writing the note again writes the same article again, with the emoji, the type and the topics chosen last.

## What changes in the body

- The images of the note become images of the article. Zenn takes PNG, JPEG, GIF and WebP images of up to 3 MB, so a note with another image cannot be written.
- A link to a file that is no image, such as a PDF, becomes its text, as Zenn cannot hold it.
- A link to another note goes to its article when that note is written as a published article; else it becomes its text.
- The dialog tells what changed, once written. Code is left as it is.

## The mark

A note written to Zenn has the mark of the letter Z with an arrow out to the top right, at the end of its row in the list; pale for a draft. The information line of the preview ends with *Zenn (draft)* or *Zenn (published)*, which opens the article on Zenn (with your user name on Zenn). The TUI shows `⇗` at the end of the row and in the information line of the preview. The marks show only while the repository of Zenn is set.

When you change the title or the body of a note after writing it, its article still has the old content. Until you write it again, the mark in the list and the button to write it in the preview have a red dot at their top right (`⇗*` in the TUI). Changing only the tags leaves no dot, as the article keeps the topics it was written with; neither does replacing only an image file, so write the note again then.

## Things to know

- Zenn has no unlisted articles from GitHub: anyone can read a published article. It can go back to a draft, but deleting it is done on the dashboard of Zenn (deleting its file from the repository leaves the article on Zenn).
- When the article file was changed in the repository since ebira wrote it, such as on GitHub, ebira asks before overwriting it.
- A note of a private notebook is confirmed before it is written.
- A note whose ID does not fit the rule of Zenn (12 to 50 of lower case letters, digits, `-` and `_`) cannot be written yet. The random IDs ebira gives fit.
- Only people write to Zenn; it is no tool of AI clients.
- The note keeps its article in `zenn:` of its file, which syncs with the notes, so another machine writes the same article again. Writing it does not change the update time of the note.
