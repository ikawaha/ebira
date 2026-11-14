---
title: コマンド
tags: [guide, terminal]
---
| コマンド | 働き |
|---|---|
| `ebira [-lang en\|ja]` | TUI を開く。サーバーが無ければ裏で起動する |
| `ebira web [-no-open]` | ブラウザで Web UI を開く。サーバーが無ければ裏で起動する |
| `ebira server start` / `stop` / `restart` / `status` | 裏のサーバーを操作する。`restart` は動いていたときのフラグを引き継ぐ |
| `ebira serve` | サーバーを前面で動かす |
| `ebira import [-notebook ID] [-force] [-dry-run] パス…` | Markdown のファイルと、それが参照する画像をノートとして取り込む |
| `ebira files check [-delete]` | 使われていない画像やファイルと、切れた参照を探す |
| `ebira sync [-remote URL]` | git と同期する（コミット、上流のブランチのマージ、push）。`-remote` で先にリポジトリにつなぐ（[同期](sync.md)） |
| `ebira auth passkey [-no-open]` | パスキーを、使い捨ての URL で登録する（[パスキー](passkey.md)） |
| `ebira auth reset [-yes]` | パスキーをすべて消し、すべてのセッションを終える（パスキーを無くしたときなど） |
| `ebira mcp add` | Claude Code に MCP のサーバーとして登録する（[AI と使う](ai.md)） |
| `ebira version` | 版を出す（作ったコミットと、`ebira server status` がサーバーについて出すビルド） |

どのコマンドも `-h` で使い方が出ます。サーバーは、ノートのディレクトリごとに 1 つだけ動き、TUI・Web UI・MCP が共有します。

## サーバーのフラグ

`ebira serve` は次のフラグを受け取ります。`ebira server start` と `ebira server restart` も同じです:

| フラグ | 既定値 | 意味 |
|---|---|---|
| `-notes` | 設定ファイルの `notes_dir`、無ければ `./notes` | ノートのディレクトリ |
| `-addr` | 設定ファイルの `addr`、無ければ `localhost:13579` | 待ち受けるアドレス |
| `-search` | ノートのディレクトリの `_ebira.yaml` の `search`、無ければ `bigram` | `like`、`bigram`、`trigram`、`en`、`ja`（[検索の書き方](search.md)） |
| `-watch` | `true` | ノートを監視して索引を更新する |
| `-lang` | 設定ファイルの `lang`、無ければブラウザの言語 | Web UI の言語: `en` か `ja`（TUI の言語は `ebira -lang` か設定ファイルの `lang`、無ければ `$LANG`） |
| `-provider`、`-model` | API キーが設定されている提供元 | アシスタントのモデル: `anthropic` か `gemini` |
| `-git-sync` | 設定ファイルの `git_sync`、無ければ同期しない | 起動時と、この間隔（`10m` など）で git と同期する |
| `-auth` | 設定ファイルの `auth`、無ければ `none` | `none` か、パスキーでログインした Web UI だけをあなたとして扱う `passkey` |

フラグの既定値は設定ファイルで決められます（[設定](settings.md)）。

## Markdown を取り込む

```sh
ebira import -notebook reading ~/Documents/notes
```

フォルダからは `.md` と `.markdown` のファイルを読みます。題名・タグ・日時は front matter から取り、ファイルが参照する画像も一緒に取り込みます。前に取り込んだファイルは、`-force` を付けなければ飛ばします。Web UI でも、Markdown のファイルをページかノートブックに落とすと取り込めます。

Quiver から移るときは、ebira のリポジトリの `scripts/quiver2ebira.py` が Quiver のライブラリを（読むだけで）Markdown に変え、`--import` を付ければ、動いているサーバーに Quiver のノートブックごとに取り込みます。ライブラリは置いた場所にあります（Finder で名前の Quiver.qvlibrary を探せます）:

```sh
scripts/quiver2ebira.py path/to/Quiver.qvlibrary /tmp/quiver-md --skip "Quiver Tutorial" --import http://localhost:13579
```
