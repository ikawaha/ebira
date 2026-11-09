---
title: コマンド
tags: [guide, terminal]
---
| コマンド | 働き |
|---|---|
| `ebira` | TUI を開く |
| `ebira web` | ブラウザで Web UI を開く |
| `ebira server start` / `stop` / `restart` / `status` | 裏のサーバーを操作する |
| `ebira serve` | サーバーを前面で動かす |
| `ebira import パス…` | Markdown のファイルを取り込む |
| `ebira files check` | 使われていない画像やファイルを探す |
| `ebira sync` | git と同期する |
| `ebira auth passkey` | パスキーを登録する |
| `ebira version` | 版を出す |

どのコマンドも `-h` で使い方が出ます。サーバーは、ノートのディレクトリごとに 1 つだけ動き、TUI・Web UI・MCP が共有します。
