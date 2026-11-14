---
title: 数式と図
tags: [guide, writing]
---
## 数式

文の中の数式は `$...$` で囲みます: $E = mc^2$

独立した数式は `$$` で囲むか、`math` のコードブロックに書きます。

$$
\int_0^\infty e^{-x^2}\,dx = \frac{\sqrt{\pi}}{2}
$$

```math
\sum_{k=1}^{n} k = \frac{n(n+1)}{2}
```

値段のような `$5` は数式になりません（閉じる `$` のすぐ後ろが数字のときなど）。

## 図

`mermaid` のコードブロックに書くと、図になります。

```mermaid
flowchart LR
  A[書く] --> B[保存]
  B --> C{探す}
  C -->|見つかる| D[読む]
  C -->|見つからない| A
```

```mermaid
sequenceDiagram
  あなた->>ebira: 検索
  ebira-->>あなた: ノート
```

書き方を誤ると、元の文字と一緒に誤りを赤く出します。
