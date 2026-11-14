---
title: 入れる・はじめる
tags: [guide, basics]
---
## 入れる

`ebira` コマンド（サーバー・Web UI・TUI）は macOS と Linux で動きます。Go 1.27 以降が要ります:

```sh
go install github.com/ikawaha/ebira/cmd/ebira@latest
```

macOS と Windows には、サーバーを中に持つデスクトップアプリもあり、Windows ではこれを使います。入れ方は [デスクトップアプリ](desktop.md) にあります。

## はじめる

ebira を起動します。初回は、ノートを置く場所、同期する git のリポジトリ（使う場合）、検索方式、Web UI と TUI の言語を聞かれ、答えは、サーバーのアドレス（`localhost:13579`）と一緒に `~/.config/ebira/config.yaml` に保存されます（MCP のクライアントに登録する URL が変わらないように）。

```console
$ ebira web
No notes found: no -notes, no notes_dir in ~/.config/ebira/config.yaml, and no notes/ in ~/work.
Let's set up ebira.

Directory to keep the notes in [~/notes]:
...
```

`ebira web` はサーバーを裏で起動し、<http://localhost:13579/> の Web UI をブラウザで開きます。`ebira` なら TUI が開きます。どちらを閉じても、サーバーは動き続けます。

デスクトップアプリは同じ設定を使うので、こうしてセットアップしてから開きます。`ebira` コマンドの無い Windows などでは、アプリの「設定…」（⌘,。Windows はファイルのメニュー）でノートのフォルダを選びます。

新しいノートのディレクトリには、このガイド（選んだ言語のもの）が入っています。上から読んで、そのノートで試し、要らなくなったらゴミ箱へ移してください。勝手に戻ってくることはありません。また読みたくなったら、歯車のメニューの「ガイドを追加」で足せます。

## 次に

- 複数の端末でノートを共有するなら、`~/Dropbox/notes` のような同期フォルダの中を答えるか、どの端末でも同じ git のリポジトリを指定します（[同期](sync.md)）。
- Markdown のファイルは `ebira import` で取り込めます。Quiver からも移せます（[コマンド](cli.md)）。
- 許したい範囲で、AI につなぎます（[AI と使う](ai.md)）。
