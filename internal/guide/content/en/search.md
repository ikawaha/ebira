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
| `pizza OR pasta` | with either (`OR` in capitals) |

Combine them: `tag:guide search -mark:red`

## Matches

The preview of a note found marks the matches. `n` / `N` go to the next or previous one.

## Search modes

The search mode is `search` in `_ebira.yaml` of the notes directory, shown in the placeholder of the search field.

- `en`: English words and their stems, so that plan finds planning.
- `ja`: Japanese analyzed into words, finding other forms of a word.
- `bigram` / `trigram`: runs of two or three characters, finding parts of words too.
