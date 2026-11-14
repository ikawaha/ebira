---
title: Writing Markdown
tags: [guide, writing]
---
Bodies are written in Markdown. A single line break is a line break too.

## Headings

The number of `#` is the level of a heading. Hover over a heading to see a copy button beside it, which copies its section as Markdown.

## Lists and tasks

- Bullets with `-`
  - Indent with Tab
1. Numbers with `1.`
- [x] Done
- [ ] To do

## Tables

| Written | Shown |
|---|---|
| `**bold**` | **bold** |
| `*italic*` | *italic* |
| `~~struck~~` | ~~struck~~ |
| `` `code` `` | `code` |

## Quotes and code

> A quote starts with `>`.

```go
func main() {
	fmt.Println("Code is colored by its language")
}
```

In the preview, the bar above a code block, with its language and number of lines, folds and unfolds it. A folded block opens when you go to a match inside it.

## Links

`[text](https://example.com)` links out. To link to another note, copy its link with the chain button after its title (or `Y`) and paste it. For example: [Welcome to ebira](welcome.md)

## Copying a note

`y` copies the whole note as Markdown, with its title as a heading: handy to give a note to an AI.
