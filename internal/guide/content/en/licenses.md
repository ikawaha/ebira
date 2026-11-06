---
title: Licenses
tags: [guide, license]
---
The licenses of ebira, and of the works of others in it.

## ebira

ebira is distributed under the GNU Affero General Public License v3.0 or later (AGPL-3.0-or-later). Its source is at https://github.com/ikawaha/ebira.

## Icon (chidori)

The icon of the tab of the web UI and of the desktop app is changed from "日本文様-千鳥模様.svg" by Pneuma01 (Wikimedia Commons, https://commons.wikimedia.org/wiki/File:%E6%97%A5%E6%9C%AC%E6%96%87%E6%A7%98-%E5%8D%83%E9%B3%A5%E6%A8%A1%E6%A7%98.svg ). It is licensed under the Creative Commons Attribution 4.0 International license (CC BY 4.0, https://creativecommons.org/licenses/by/4.0/ ), not under the license of ebira. Changes: the editor's metadata is removed, the picture is centered on a white square with rounded corners, and the desktop app adds a margin around it.

## Parts of the web UI

| Part | Version | License |
|---|---|---|
| marked | 18.1.1 | MIT |
| DOMPurify | 3.4.17 | Apache-2.0 or MPL-2.0 |
| KaTeX | 0.19.0 | MIT |
| Mermaid | 12.2.0 | MIT |
| CodeMirror 6 | (by package) | MIT |
| gemoji (the list of emoji) | 4.1.0 | MIT |

The full text of each license is in `internal/ui/web/static/vendor/*.LICENSE` of the source.

## Main parts of the server and the TUI

| Part | License |
|---|---|
| kagome (Japanese morphological analysis) | MIT |
| The IPA dictionary of kagome-dict | MIT (its data: the notice of mecab-ipadic below) |
| modernc.org/sqlite | BSD-3-Clause |
| Goa, Goa-AI | MIT |
| Bubble Tea, Bubbles, Lip Gloss, Glamour | MIT |
| fsnotify | BSD-3-Clause |
| go-webauthn | BSD-3-Clause |
| snowball (English stems) | MIT |
| The Anthropic SDK | MIT |
| The Google Gen AI SDK, OpenTelemetry | Apache-2.0 |

The other parts, used through these, are in `go.mod` and `go.sum` of the source, each under its own license.

## Parts of the desktop app

| Part | License |
|---|---|
| Wails v3 | MIT |

## The notice of mecab-ipadic

The dictionary data of the Japanese search (`ja`) comes from mecab-ipadic-2.7.0-20070801. Its notice, as it is:

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
