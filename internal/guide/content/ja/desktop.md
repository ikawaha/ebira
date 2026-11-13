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

**Windows**（PowerShell で作ります）:

1. Go を https://go.dev/dl/ から入れる
2. ebira のソースを手に入れる（`git clone https://github.com/ikawaha/ebira.git`。git が無ければ、GitHub のページの「Code」→「Download ZIP」で落として展開する）
3. PowerShell でそのフォルダに移り、次を実行する

```powershell
powershell -ExecutionPolicy Bypass -File apps\desktop\build-windows.ps1
```

`apps\desktop\build\windows-amd64\ebira\` ができます（ARM の Windows なら、先に `$env:GOARCH = "arm64"`）。中の `ebira-desktop.exe` を開きます。フォルダごと別の場所や別の PC に写しても使えます。Windows 10 では WebView2 ランタイムを入れる必要があることがあります。

macOS や Linux の上でも、`apps/desktop/build-windows.sh` で Windows 用のフォルダを作れます。

## 起動したとき

アプリは、設定のアドレス（既定は `localhost:13579`）で、同じノートのディレクトリのサーバーが動いていればそれを使い、無ければ中のサーバーを起動します。アプリを終えても、サーバーは動いたまま残ります（`ebira server stop` で止まります）。

つなげないときは、設定のウインドウに理由が出ます。

## 設定

ebira のメニューの「設定…」（⌘,。Windows はファイルのメニュー）は、Web UI と同じ設定のダイアログを開きます（[設定](settings.md)）。その「アプリ」のタブの「変える…」で開くアプリの設定のウインドウで、ノートのフォルダとサーバーのアドレスを変えます。サーバーにつながらないときは、「設定…」でこのウインドウが開きます。ノートのフォルダが空なら、設定ファイルの `notes_dir` に従います（[設定](settings.md)）。「適用」を押すと、そのノートのサーバーにつなぎ直します。

サーバーのアドレスに別のコンピューターのものを選ぶと、アプリはそのサーバーのページをそのまま開きます。そのページには、クリップボードへの書き込みや、ブラウザでリンクを開くことを任せることになります。自分で動かしているなど、信頼できるサーバーだけを選んでください。

歯車のメニューは、アプリのメニューにあるものを出さず、パスキーでログインしているときだけ出ます。

## メニュー

ノートの編集・保存・探す・マーク・履歴・コピーなどは、メニューからも使えます（ショートカットは Web UI のキーと同じ働き）。表示しているノートとノートブックは、「ノート」と「ノートブック」のメニューの「アーカイブへ移す」でアーカイブへ移せ、ファイルのメニューの「アーカイブの候補…」で候補を開けます（[ノートとノートブック](notes.md)）。表示のメニューの「一覧を絞り込む」で、検索欄の検索と絞り込みを切り替えます（絞り込みの間はチェックが付きます。[検索の書き方](search.md)）。「ヘルプ」から、このガイドと[検索の書き方](search.md)、AI とつなぐコマンド（[AI と使う](ai.md)）を開けます。

アプリより古いサーバーにつないでいると、右上の接続の表示の横に ↻ の印が出ます。押すと、サーバーをアプリの中のもので起動し直します。

「ebira について」に、アプリの版と、つないでいるサーバーの版・アドレス・ノートのディレクトリが出ます。

## ログ

うまく動かないときは、アプリのログと、サーバーのログを見てください。

- アプリ: macOS は `~/Library/Logs/ebira/desktop.log`、Windows は `%LocalAppData%\ebira\desktop.log`。1 MiB を超えると `desktop.log.1` に移して新しく始めるので、合わせて 2 MiB ほどで止まります。
- サーバー: ノートのディレクトリごとの状態の場所の `server.log`（`ebira server status` が場所を出します）。
