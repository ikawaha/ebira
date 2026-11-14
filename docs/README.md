# ebira documentation

- [English](en/README.md)
- [日本語](ja/README.md)

These pages are also the guide that ebira puts in a new notes directory.

このページたちは、ebira が新しいノートのディレクトリに入れるガイドでもあります。

## Editing these pages

Each language has the same files in its folder. `guide.yaml` tells their order in the guide, with the separators and the sub notebook, and `README.md` lists them in that order with a line on each (a test checks that the list follows `guide.yaml`). A page links to another by its file name, such as `[How to search](search.md)`, which becomes a link to the note in the guide. When a page changes, raise `Version` in `internal/guide/guide.go`.
