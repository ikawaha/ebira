---
title: Math and diagrams
tags: [guide, writing]
---
## Math

Inline math goes between `$...$`: $E = mc^2$

Display math goes between `$$`, or in a `math` code block.

$$
\int_0^\infty e^{-x^2}\,dx = \frac{\sqrt{\pi}}{2}
$$

```math
\sum_{k=1}^{n} k = \frac{n(n+1)}{2}
```

A price like `$5` stays text, as when a digit follows the closing `$`.

## Diagrams

A `mermaid` code block becomes a diagram.

```mermaid
flowchart LR
  A[Write] --> B[Save]
  B --> C{Search}
  C -->|found| D[Read]
  C -->|not found| A
```

```mermaid
sequenceDiagram
  You->>ebira: Search
  ebira-->>You: Notes
```

A mistake shows in red, with the text as written.
