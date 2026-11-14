# ebira documentation

The documentation of ebira, in English and Japanese. It is also the guide that ebira puts in a new notes directory, a notebook of these notes.

ebira のドキュメントです（英語と日本語）。ebira が新しいノートのディレクトリに入れるガイドのノートブックも、このページたちです。

## English

- Getting started: [Welcome to ebira](en/welcome.md) · [Installing and setting up](en/install.md) · [The screen](en/screen.md) · [Desktop App](en/desktop.md) · [Notes and notebooks](en/notes.md)
- Writing: [Writing Markdown](en/markdown.md) · [Math and diagrams](en/math.md) · [Images and files](en/images.md) · [Handy keys of the editor](en/editor.md)
- Organizing: [Tags](en/tags.md) · [Marks](en/marks.md) · [Separators and order](en/separators.md)
- Finding: [How to search](en/search.md) · [Finding in a note](en/find.md)
- Keeping safe: [History](en/history.md) · [Kinds of notebooks and AI](en/kinds.md)
- Connecting: [Using AI](en/ai.md) · [Syncing](en/sync.md) · [Passkeys](en/passkey.md)
- Look and settings: [Settings](en/settings.md) · [Licenses](en/licenses.md)
- Terminal: [TUI](en/tui.md) · [Commands](en/cli.md)

## 日本語

- はじめに: [ようこそ ebira へ](ja/welcome.md)・[入れる・はじめる](ja/install.md)・[画面の見方](ja/screen.md)・[デスクトップアプリ](ja/desktop.md)・[ノートとノートブック](ja/notes.md)
- 書く: [Markdown の書き方](ja/markdown.md)・[数式と図](ja/math.md)・[画像とファイル](ja/images.md)・[編集欄の便利な操作](ja/editor.md)
- 整理する: [タグ](ja/tags.md)・[マーク](ja/marks.md)・[区切り線と並び順](ja/separators.md)
- 探す: [検索の書き方](ja/search.md)・[ノートの中を探す](ja/find.md)
- 守る・戻す: [履歴](ja/history.md)・[ノートブックの種別と AI](ja/kinds.md)
- つなぐ: [AI と使う](ja/ai.md)・[同期](ja/sync.md)・[パスキー](ja/passkey.md)
- 見た目と設定: [設定](ja/settings.md)・[ライセンス](ja/licenses.md)
- ターミナル: [TUI](ja/tui.md)・[コマンド](ja/cli.md)

## Editing these pages

Each language has the same files. `guide.yaml` of each tells their order in the guide, with the separators and the sub notebook. A page links to another by its file name, such as `[How to search](search.md)`, which becomes a link to the note in the guide. When a page changes, raise `Version` in `internal/guide/guide.go`, and keep this list in step with `guide.yaml` (a test checks it).
