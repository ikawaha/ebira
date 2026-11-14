---
title: ライセンス
tags: [guide, license]
---
ebira と、ebira に入っているほかの人の作品のライセンスです。

## ebira

ebira は GNU Affero General Public License v3.0 以降（AGPL-3.0-or-later）で配っています。ソースは https://github.com/ikawaha/ebira にあります。

## アイコン（千鳥）

Web UI のタブとデスクトップアプリのアイコンは、Pneuma01 による「日本文様-千鳥模様.svg」（Wikimedia Commons、https://commons.wikimedia.org/wiki/File:%E6%97%A5%E6%9C%AC%E6%96%87%E6%A7%98-%E5%8D%83%E9%B3%A5%E6%A8%A1%E6%A7%98.svg ）を改変したものです。ライセンスは Creative Commons Attribution 4.0 International（CC BY 4.0、https://creativecommons.org/licenses/by/4.0/ ）で、ebira のライセンスの対象ではありません。改変: 編集用の情報を除き、白い角丸の四角の真ん中に置き、デスクトップアプリでは周りに余白を足しました。Web UI の AI が作ったノートの印も、同じ絵（白い四角を除き、羽と目を抜いて文字の色で描いたもの）です。

## Web UI に入っている部品

| 部品 | 版 | ライセンス |
|---|---|---|
| marked | 18.1.1 | MIT |
| DOMPurify | 3.4.17 | Apache-2.0 または MPL-2.0 |
| KaTeX | 0.19.0 | MIT |
| Mermaid | 12.2.0 | MIT |
| CodeMirror 6 | （部品ごと） | MIT |
| gemoji（絵文字の一覧） | 4.1.0 | MIT |

それぞれのライセンスの全文は、ソースの `internal/ui/web/static/vendor/*.LICENSE` にあります。

## サーバーと TUI に入っている主な部品

| 部品 | ライセンス |
|---|---|
| kagome（日本語の形態素解析） | MIT |
| kagome-dict の IPA 辞書 | MIT（辞書のデータは下の mecab-ipadic の告知） |
| modernc.org/sqlite | BSD-3-Clause |
| Goa・Goa-AI | MIT |
| Bubble Tea・Bubbles・Lip Gloss・Glamour | MIT |
| fsnotify | BSD-3-Clause |
| go-webauthn | BSD-3-Clause |
| snowball（英語の語幹） | MIT |
| Anthropic の SDK | MIT |
| Google の Gen AI SDK・OpenTelemetry | Apache-2.0 |

ほかの部品（間接に使うもの）は、ソースの `go.mod` と `go.sum` にあり、それぞれのライセンスに従います。

## デスクトップアプリに入っている部品

| 部品 | ライセンス |
|---|---|
| Wails v3 | MIT |

## mecab-ipadic の告知

日本語の検索（`ja`）が使う辞書のデータは mecab-ipadic-2.7.0-20070801 によります。その告知を元の英文のまま載せます。

```
mecab-ipadic-2.7.0-20070801 Notice
===========================================================================

Nara Institute of Science and Technology (NAIST),
the copyright holders, disclaims all warranties with regard to this
software, including all implied warranties of merchantability and
fitness, in no event shall NAIST be liable for
any special, indirect or consequential damages or any damages
whatsoever resulting from loss of use, data or profits, whether in an
action of contract, negligence or other tortuous action, arising out
of or in connection with the use or performance of this software.

A large portion of the dictionary entries
originate from ICOT Free Software.  The following conditions for ICOT
Free Software applies to the current dictionary as well.

Each User may also freely distribute the Program, whether in its
original form or modified, to any third party or parties, PROVIDED
that the provisions of Section 3 ("NO WARRANTY") will ALWAYS appear
on, or be attached to, the Program, which is distributed substantially
in the same form as set out herein and that such intended
distribution, if actually made, will neither violate or otherwise
contravene any of the laws and regulations of the countries having
jurisdiction over the User or the intended distribution itself.

NO WARRANTY

The program was produced on an experimental basis in the course of the
research and development conducted during the project and is provided
to users as so produced on an experimental basis.  Accordingly, the
program is provided without any warranty whatsoever, whether express,
implied, statutory or otherwise.  The term "warranty" used herein
includes, but is not limited to, any warranty of the quality,
performance, merchantability and fitness for a particular purpose of
the program and the nonexistence of any infringement or violation of
any right of any third party.

Each user of the program will agree and understand, and be deemed to
have agreed and understood, that there is no warranty whatsoever for
the program and, accordingly, the entire risk arising from or
otherwise connected with the program is assumed by the user.

Therefore, neither ICOT, the copyright holder, or any other
organization that participated in or was otherwise related to the
development of the program and their respective officials, directors,
officers and other employees shall be held liable for any and all
damages, including, without limitation, general, special, incidental
and consequential damages, arising out of or otherwise in connection
with the use or inability to use the program or any product, material
or result produced or otherwise obtained by using the program,
regardless of whether they have been advised of, or otherwise had
knowledge of, the possibility of such damages at any time during the
project or thereafter.  Each user will be deemed to have agreed to the
foregoing by his or her commencement of use of the program.  The term
"use" as used herein includes, but is not limited to, the use,
modification, copying and distribution of the program and the
production of secondary products from the program.

In the case where the program, whether in its original form or
modified, was distributed or delivered to or received by a user from
any person, organization or entity other than ICOT, unless it makes or
grants independently of ICOT any specific warranty to the user in
writing, such person, organization or entity, will also be exempted
from and not be held liable to the user for any such damages as noted
above as far as the program is concerned.
```
