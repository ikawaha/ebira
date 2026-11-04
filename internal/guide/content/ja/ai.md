---
title: AI と使う
tags: [guide, ai]
---
## MCP

ebira は MCP のサーバーです。AI のクライアントに次の URL を登録すると、AI がノートを探し、読み、collab のノートブックに書けます（[ノートブックの種別と AI](kinds.md)）。

```
http://localhost:13579/mcp
```

Claude Code なら、次のコマンドで登録できます。

```
claude mcp add --transport http ebira http://localhost:13579/mcp
```

アドレスを変えていれば、そのアドレスにしてください。

## アシスタント

`a` で開く右の欄では、ノートを探して読み、質問に答えます。サーバーを起動するときに、環境変数 `ANTHROPIC_API_KEY`（Claude）か `GEMINI_API_KEY`（Gemini）があると使えます。答えには、使ったノートへのリンクが付きます。

## AI にノートを示す

プレビューの題名の後ろの鎖の印（か `Y`）で、ノートの URL か Markdown のリンクをコピーできます。URL の `#note/` の後ろがノートの ID で、AI はその ID でノートを読めます。
