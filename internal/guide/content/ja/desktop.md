---
title: デスクトップアプリ
tags: [guide]
---
Web UI をブラウザのタブではなく、自分のウインドウとメニューで使うアプリです。macOS と Windows で動きます。中身は Web UI と同じで、ノートのディレクトリとサーバーも、TUI・CLI・MCP と共有します。

## 作る

今はまだ配布していないので、ebira のリポジトリから作ります。Go 1.27 以降が要ります。

**macOS**（macOS 26 以降。Xcode の Command Line Tools が要ります）:

```sh
git clone https://github.com/ikawaha/ebira.git
cd ebira
apps/desktop/build.sh
open apps/desktop/build/ebira.app
```

`apps/desktop/build/ebira.app` ができます。中にサーバー（`ebira`）が入っていて、この Mac のためだけに署名してあります。アプリケーションのフォルダに移して使えます。

**Windows**（macOS や Linux の上で作ります）:

```sh
apps/desktop/build-windows.sh
```

`apps/desktop/build/windows-amd64/ebira/` ができます（ARM なら `GOARCH=arm64` を付けて実行）。フォルダごと Windows に写し、`ebira-desktop.exe` を開きます。Windows 10 では WebView2 ランタイムを入れる必要があることがあります。

## 起動したとき

アプリは、設定のアドレス（既定は `localhost:13579`）で、同じノートのディレクトリのサーバーが動いていればそれを使い、無ければ中のサーバーを起動します。アプリを終えても、サーバーは動いたまま残ります（`ebira server stop` で止まります）。

つなげないときは、設定のウインドウに理由が出ます。

## 設定

ebira のメニューの「設定…」（⌘,。Windows はファイルのメニュー）で、ノートのフォルダとサーバーのアドレスを変えます。ノートのフォルダが空なら、設定ファイルの `notes_dir` に従います（[設定](settings.md)）。「適用」を押すと、そのノートのサーバーにつなぎ直します。

サーバーの設定（言語・検索方式など）は、「サーバーの設定…」で変えます。Web UI の歯車のメニューと同じものです。

## メニュー

ノートの編集・保存・探す・マーク・履歴・コピーなどは、メニューからも使えます（ショートカットは Web UI のキーと同じ働き）。「ヘルプ」から、このガイドと[検索の書き方](search.md)を開けます。

アプリより古いサーバーにつないでいると、右上の接続の表示の横に ↻ の印が出ます。押すと、サーバーをアプリの中のもので起動し直します。

「ebira について」に、アプリの版と、つないでいるサーバーの版・アドレス・ノートのディレクトリが出ます。
