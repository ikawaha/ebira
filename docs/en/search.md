---
title: How to search
tags: [guide, search]
---
`/` moves to the search field, which searches as you type. The "?" at its right end explains how to write a search.

| Search | Finds the notes |
|---|---|
| `note search` | with both words |
| `"search notes"` | with the words in this order (a phrase) |
| `tag:guide` | with the tag |
| `mark:red` | with the mark |
| `-draft` | without the word (also `-tag:` and `-mark:`) |
| `in:archive` | in the archive (other searches leave the archive out) |
| `pizza OR pasta` | with either (`OR` in capitals) |

Combine them: `tag:guide search -mark:red`

Searches leave out the trash and the archive. Choose the Trash or the archive in the side column and search to search there ([Notes and notebooks](notes.md)).

## Filter

The funnel at the right of the search box (struck out while it searches) turns it into a filter, which takes the same query and narrows the list shown, kept in its order (the box then says "Filter"). How it differs from a search:

| | Search | Filter |
|---|---|---|
| Order | Best match first | The order of the list (by hand, or its sort) |
| With a notebook chosen | Notes of its sub notebooks mixed in | Sub notebooks stay as cells, filtered too |
| Separators | Not shown | Kept |
| Matches marked | Titles and snippets of the body | Titles alone (and the preview) |
| Notes listed | The top 200 | All, 500 at a time (then "Load more") |
| Reordering | No | Yes, in the manual order |
| Note shown | The first one found | The one shown, while the list has it |

To find something, search; to see part of a list in the order you gave it, such as by priority, filter.

- The order given by hand, the separators and the cells of sub notebooks stay; only the notes not matching are hidden. An open cell shows how many of its notes are shown, out of all.
- For instance, `-tag:renovate` leaves out the notes with the tag, and the rest stay in their order.
- In the manual order, the filtered list can still be reordered by dragging or with `J` / `K`, among the notes shown.
- It works in any list, and this browser remembers it. Click the funnel again to search.
- In the desktop app, "Filter the List" in the View menu switches it too; in the TUI, Tab in the search box ([TUI](tui.md)).

## Matches

The preview of a note found marks the matches. `n` / `N` go to the next or previous one.

## Search modes

The search mode is `search` in `_ebira.yaml` of the notes directory, shown in the placeholder of the search field.

| Mode | How | Good for |
|---|---|---|
| `bigram` (the default) | Splits text into overlapping pairs of characters | Any language; finds any substring of two or more characters |
| `trigram` | Splits text into overlapping runs of three characters | Any language, with fewer matches for long terms; a term of one or two characters, such as many Japanese words, is searched through every note, more slowly |
| `ja` | Splits Japanese into words with kagome, with the verbs and adjectives in their base form, and also searches bigrams | Notes in Japanese: a search for a verb finds its other forms |
| `en` | Takes English words with their stems, without stop words such as *the* and *of*, and Japanese as bigrams; also searches trigrams, for parts of words | Notes in English: *plan* finds *plans* and *planning*, and a word being typed is still found |
| `like` | SQL `LIKE` | Small collections |

Text is normalized with NFKC and lowercased, so full-width and half-width characters, and upper and lower case, match each other. Set the mode for all your machines in `_ebira.yaml`:

```yaml
search: ja
```
