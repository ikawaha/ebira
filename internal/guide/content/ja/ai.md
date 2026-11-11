---
title: AI と使う
tags: [guide, ai]
---
## MCP

ebira は MCP のサーバーです。AI のクライアントに ebira の URL を登録すると、AI がノートを探し、読み、collab のノートブックに書けます（[ノートブックの種別と AI](kinds.md)）。private のノートブックは見えません。

サーバーの URL と Claude Code に登録するコマンドは、歯車のメニューの「AI とつなぐ（MCP）…」（デスクトップアプリはヘルプのメニュー）に、今のサーバーのアドレスで出ます。コピーのアイコンで写せます。

AI がつなげるのは、ebira のサーバーが動いている間だけです。サーバーのアドレス（設定ファイルの `addr`）を変えたら、登録し直してください。

## Claude Code に登録する

どれも同じ登録をします。ユーザーのスコープ（`--scope user`）に登録するので、どのプロジェクトからも使えます。

- **Web UI**: 歯車のメニューの「AI とつなぐ（MCP）…」の「Claude Code に登録」（デスクトップアプリはヘルプのメニュー）。
- **ターミナル**: `ebira mcp add`。登録するコマンドを見せて、確かめてから実行します（`-yes` で確かめずに、`-print` で出すだけ）。
- **自分で**: 次のコマンド（アドレスを変えていれば、そのアドレス）。

```
claude mcp add --transport http --scope user ebira http://localhost:13579/mcp
```

登録したあとに始めた Claude Code のセッションから使えます。登録されているかは `claude mcp list` か、Claude Code の中の `/mcp` で確かめられます。アドレスを変えたあとは、ボタンか `ebira mcp add` で、前のアドレスの登録を新しいアドレスに置き換えられます。

### ボタンが出ないとき

ボタンは、ebira のサーバーが Claude Code（`claude`）を見つけられるときだけ出ます。サーバーがその機械の `claude` を実行して登録するためです。

- Claude Code が入っていない: 入れてから、ダイアログを開き直してください。
- 別の機械のブラウザで開いている: ボタンはサーバーの機械の Claude Code に登録するので、出しません。使う機械のターミナルでコマンドを実行してください。

### プロジェクトごとの登録

前にローカルのスコープ（`--scope local`。そのプロジェクトだけで、`claude mcp add` の既定）で登録していると、そのプロジェクトではそちらが使われます。そのプロジェクトのディレクトリで `claude mcp remove ebira -s local` を実行すると、ユーザーのスコープの登録が使われます。

## Claude Code にノートのファイルを触らせない

ノートブックの種別（private など）の決まりは、ebira の MCP を通るときにだけ効きます。Claude Code はファイルを読み書きする道具やシェルも持っているので、ノートのディレクトリを直接読めば、private のノートも見えます。MCP だけでノートを扱わせるには、Claude Code の設定で、ノートのディレクトリを読む・書くことを禁じます。

ユーザーの設定 `~/.claude/settings.json` に次のように書くと、どのプロジェクトでも効きます（`~/notes` は、自分のノートのディレクトリ（設定ファイルの `notes_dir`）に置き換えてください）。

```json
{
  "permissions": {
    "allow": ["mcp__ebira__*"],
    "deny": ["Read(~/notes/**)", "Edit(~/notes/**)"],
    "disableBypassPermissionsMode": "disable"
  },
  "sandbox": {
    "enabled": true,
    "allowUnsandboxedCommands": false,
    "failIfUnavailable": true,
    "filesystem": {
      "denyRead": ["~/notes"],
      "denyWrite": ["~/notes"]
    }
  }
}
```

- `permissions.deny`: ファイルを読む道具（Read・Grep・Glob と、シェルの `cat` などのよく使うコマンド）と、書く道具（Edit・Write など）に、ノートのディレクトリを使わせません。deny はほかの設定の allow より先に効きます。パスは `~/` か `//`（絶対パス）で書きます。`/` 1 つで始めると、設定ファイルの場所からのパスになります。
- `permissions.allow`: ebira の MCP のツールを、毎回確かめずに使わせます（確かめたいなら書かない）。
- `sandbox`: シェルのコマンドを OS のサンドボックスの中で動かし、ノートのディレクトリを読み書きさせません。deny の決まりは、ファイルの名前を書かないコマンド（ノートのディレクトリでの `grep -r`）や、自分でファイルを開くスクリプト（Python など）には効かないので、これで防ぎます。`allowUnsandboxedCommands: false` は、サンドボックスの外でやり直すことを、`failIfUnavailable` は、サンドボックスが使えないときに外で動かすことを止めます。
- `disableBypassPermissionsMode`: 確かめと決まりを飛ばすモード（`--dangerously-skip-permissions`）を使えなくします。

ebira の MCP のサーバーは、Claude Code の外で動く別のプロセスなので、これらの決まりに関係なくノートを扱えます（それが目的です）。

- サンドボックスは macOS・Linux・WSL2 で動きます。Windows では、シェルのコマンドはサンドボックスなしで動くので、deny の決まりだけになります。
- Claude Code のプロンプトで `!` を付けて自分で打ったコマンドは、サンドボックスの外で動きます。
- ノートのディレクトリの中で Claude Code を起動しないでください（そこが作業のディレクトリになります）。
- 設定の書き方は Claude Code の版で変わることがあります。詳しくは Claude Code の説明（permissions と sandboxing）を見てください。

## ほかのクライアント

MCP のサーバーを JSON の設定ファイルに書くクライアント（Claude Code のプロジェクトの `.mcp.json` など）では、次の形で書きます。書き方はクライアントによって少し違います。`ebira mcp add -print` も、今のサーバーのアドレスでこの形を出します。

```json
{
  "mcpServers": {
    "ebira": {
      "type": "http",
      "url": "http://localhost:13579/mcp"
    }
  }
}
```

## アシスタント

`a` で開く右の欄では、ノートを探して読み、質問に答えます。サーバーを起動するときに、環境変数 `ANTHROPIC_API_KEY`（Claude）か `GEMINI_API_KEY`（Gemini）があると使えます。答えには、使ったノートへのリンクが付きます。

## AI にノートを示す

プレビューの題名の後ろの鎖の印（か `Y`）で、ノートの URL か Markdown のリンクをコピーできます。URL の `#note/` の後ろがノートの ID で、AI はその ID でノートを読めます。
