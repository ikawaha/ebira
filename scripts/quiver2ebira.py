#!/usr/bin/env python3
"""quiver2ebira.py moves the notes of a Quiver library into ebira.

It converts each notebook of the library into a folder of Markdown files with
a front matter (title, tags, created, updated, quiver_uuid) and the images of
its notes, and, with --import, imports them into an ebira server: a notebook
of the same name is made for each, Quiver's Inbox goes into ebira's inbox,
and ebira import adds the notes with their images. The library is only read.

Usage:
  scripts/quiver2ebira.py LIBRARY OUT                 convert into OUT, a new or empty directory
  scripts/quiver2ebira.py LIBRARY OUT --import URL    convert, then import into the server at URL
                                                      (such as http://localhost:13579)

Flags:
  --skip NAME    leave out the notebook NAME (repeatable), such as "Quiver Tutorial"
  --ebira PATH   the ebira command for --import (default: ebira on PATH)

The cells of a note become Markdown: markdown as it is, text (HTML) as it
is, code as a fenced code block, LaTeX as $$ math, and the diagrams
(js-sequence-diagrams and flowchart.js) as Mermaid. Images referred to as
quiver-image-url/<name> point to the note's resources, which ebira import
copies next to the note. The Trash of Quiver and empty notebooks are left
out. A note whose content was imported before is skipped by ebira import, as
it is for any import (rerunning --import into the same notes adds nothing).
"""
import argparse
import glob
import json
import os
import re
import shutil
import subprocess
import sys
import urllib.request
from datetime import datetime, timezone

IMG = re.compile(r'quiver-image-url/([^)\s"\']+)(\s+=\d+x\d+)?')


def mm_text(s):
    return s.replace('#', '#35;').replace(';', '#59;').strip()


def seq_to_mermaid(src):
    """js-sequence-diagrams (Quiver) to a Mermaid sequence diagram."""
    actors, lines, out = {}, [], []

    def actor(name):
        name = name.strip().strip('"')
        if name not in actors:
            actors[name] = f'A{len(actors) + 1}'
        return actors[name]

    arrow = re.compile(r'^(.+?)\s*(-->>|->>|-->|->)\s*(.+?)\s*:\s*(.*)$')
    for raw in src.splitlines():
        line = raw.strip()
        if not line:
            continue
        m = re.match(r'(?i)^title\s*:\s*(.*)$', line)
        if m:
            out.append('    title ' + mm_text(m.group(1)))
            continue
        m = re.match(r'(?i)^participant\s+(?:"(.+)"|(\S+))(?:\s+as\s+(\S+))?$', line)
        if m:
            actor(m.group(3) or m.group(1) or m.group(2))
            continue
        m = re.match(r'(?i)^note\s+(left of|right of|over)\s+([^:]+):\s*(.*)$', line)
        if m:
            who = ','.join(actor(a) for a in m.group(2).split(','))
            lines.append(f'    Note {m.group(1).lower()} {who}: ' + mm_text(m.group(3)))
            continue
        m = arrow.match(line)
        if m:
            a, kind, b, msg = actor(m.group(1)), m.group(2), actor(m.group(3)), mm_text(m.group(4))
            mk = {'->': '->>', '-->': '-->>', '->>': '-)', '-->>': '--)'}[kind]
            lines.append(f'    {a}{mk}{b}: {msg}')
            continue
        lines.append('    %% ' + line)
    head = ['sequenceDiagram'] + out
    head += [f'    participant {i} as {mm_text(n)}' for n, i in actors.items()]
    return '\n'.join(head + lines)


def flow_to_mermaid(src):
    """flowchart.js (Quiver) to a Mermaid flowchart."""
    defs, order, edges, last = {}, [], [], None
    for raw in src.splitlines():
        if '=>' in raw:
            k, rest = raw.split('=>', 1)
            typ, _, label = rest.partition(':')
            last = k.strip()
            defs[last] = [typ.strip(), label]
            order.append(last)
        elif '->' in raw:
            parts = [x.strip() for x in raw.split('->')]
            for a, b in zip(parts, parts[1:]):
                m = re.match(r'^([^(]+)(?:\((\w+)(?:,\s*\w+)?\))?$', a)
                bn = re.match(r'^([^(]+)', b).group(1)
                cond = m.group(2) if m and m.group(2) in ('yes', 'no') else None
                edges.append((m.group(1) if m else a, cond, bn))
        elif last and raw.strip():
            defs[last][1] += '\n' + raw
    shapes = {'start': ('([', '])'), 'end': ('([', '])'), 'operation': ('[', ']'), 'subroutine': ('[[', ']]'),
              'condition': ('{', '}'), 'inputoutput': ('[/', '/]'), 'parallel': ('[', ']')}
    out = ['flowchart TD']
    for k in order:
        typ, label = defs[k]
        label = re.sub(r'(^|:)>\S+', '', label).strip() or typ.capitalize()
        label = label.replace('"', '#quot;').replace('\n', '<br/>')
        o, c = shapes.get(typ, ('[', ']'))
        out.append(f'    {k}{o}"{label}"{c}')
    for a, cond, b in edges:
        out.append(f'    {a} -->|{cond}| {b}' if cond else f'    {a} --> {b}')
    return '\n'.join(out)


def latex_to_md(d):
    """A Quiver LaTeX cell, text with environments and $...$, to Markdown math."""
    return re.sub(r'(\\begin\{(\w+\*?)\}.*?\\end\{\2\})', lambda m: '\n$$\n' + m.group(1) + '\n$$\n', d, flags=re.S)


def fix_images(s):
    return IMG.sub(lambda m: 'resources/' + m.group(1), s)


def cell_md(c):
    t, d = c['type'], c.get('data', '')
    if t in ('markdown', 'text'):  # text cells are HTML, which Markdown allows
        return fix_images(d)
    if t == 'code':
        return '```' + c.get('language', '') + '\n' + d.rstrip('\n') + '\n```'
    if t == 'latex':
        return latex_to_md(d)
    if t == 'diagram':
        kind = c.get('diagramType')
        mm = seq_to_mermaid(d) if kind == 'sequence' else flow_to_mermaid(d) if kind == 'flow' else d
        return '```mermaid\n' + mm.rstrip('\n') + '\n```'
    return d


def iso(ts):
    return datetime.fromtimestamp(ts, timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ')


def read_json(path):
    with open(path, encoding='utf-8') as f:
        return json.load(f)


def convert(lib, out, skip):
    """Converts the notebooks of lib into numbered folders of out and returns
    the plan: the name, folder and number of notes of each notebook."""
    names, plan = {}, []
    for nbd in sorted(glob.glob(os.path.join(lib, '*.qvnotebook'))):
        name = read_json(os.path.join(nbd, 'meta.json'))['name']
        notes = sorted(glob.glob(os.path.join(nbd, '*.qvnote')))
        if not notes or name == 'Trash' or name in skip:
            print(f'skip notebook {name} ({len(notes)} notes)')
            continue
        # Quiver allows notebooks of one name; ebira does not.
        names[name] = names.get(name, 0) + 1
        disp = name if names[name] == 1 else f'{name} {names[name]}'
        folder = os.path.join(out, str(len(plan)))
        os.makedirs(folder)
        plan.append({'name': disp, 'folder': folder, 'notes': len(notes)})
        for i, n in enumerate(notes):
            meta = read_json(os.path.join(n, 'meta.json'))
            content = read_json(os.path.join(n, 'content.json'))
            fm = ['---', 'title: ' + json.dumps(meta.get('title') or content.get('title') or 'Untitled', ensure_ascii=False)]
            if meta.get('tags'):
                fm.append('tags: ' + json.dumps(meta['tags'], ensure_ascii=False))
            fm += ['created: ' + iso(meta['created_at']), 'updated: ' + iso(meta['updated_at']),
                   'quiver_uuid: ' + meta['uuid'], '---', '']
            body = '\n\n'.join(cell_md(c) for c in content.get('cells', []))
            with open(os.path.join(folder, f'{i:03d}.md'), 'w', encoding='utf-8') as f:
                f.write('\n'.join(fm) + body + '\n')
            res = os.path.join(n, 'resources')
            if os.path.isdir(res):
                os.makedirs(os.path.join(folder, 'resources'), exist_ok=True)
                for r in os.listdir(res):
                    shutil.copy(os.path.join(res, r), os.path.join(folder, 'resources', r))
        print(f'converted {disp}: {len(notes)} notes')
    with open(os.path.join(out, 'plan.json'), 'w', encoding='utf-8') as f:
        json.dump(plan, f, ensure_ascii=False, indent=1)
    return plan


def notebook_for(server, name, existing):
    """Returns the ID of the notebook to import into: inbox for Quiver's Inbox,
    a notebook of that name if there is one, else a new one."""
    if name == 'Inbox':
        return 'inbox'
    for nb in existing:
        if nb['name'] == name and not nb.get('parent'):
            return nb['id']
    req = urllib.request.Request(server + '/notebooks', json.dumps({'name': name}).encode(),
                                 {'Content-Type': 'application/json'})
    with urllib.request.urlopen(req) as r:
        return json.load(r)['id']


def import_plan(plan, server, ebira):
    """Imports the converted notebooks into the ebira server."""
    server = server.rstrip('/')
    with urllib.request.urlopen(server + '/notebooks') as r:
        existing = json.load(r)
    failed = False
    for p in plan:
        nb = notebook_for(server, p['name'], existing)
        print(f'== {p["name"]} -> {nb}', flush=True)
        r = subprocess.run([ebira, 'import', '-server', server, '-notebook', nb, p['folder']])
        failed = failed or r.returncode != 0
    return not failed


def main():
    ap = argparse.ArgumentParser(description=__doc__.split('\n\n')[0])
    ap.add_argument('library', help='the Quiver library (.qvlibrary)')
    ap.add_argument('out', help='a new or empty directory for the Markdown files')
    ap.add_argument('--import', dest='server', metavar='URL', help='import into the ebira server at URL')
    ap.add_argument('--skip', action='append', default=[], metavar='NAME', help='leave out the notebook NAME')
    ap.add_argument('--ebira', default='ebira', help='the ebira command for --import')
    args = ap.parse_args()

    if not os.path.isdir(args.library) or not glob.glob(os.path.join(args.library, '*.qvnotebook')):
        sys.exit(f'quiver2ebira: {args.library} is not a Quiver library')
    # OUT is never emptied, so that a wrong path cannot lose files.
    if os.path.exists(args.out) and os.listdir(args.out):
        sys.exit(f'quiver2ebira: {args.out} is not empty; give a new or empty directory')
    os.makedirs(args.out, exist_ok=True)
    plan = convert(args.library, args.out, set(args.skip))
    print(f'{sum(p["notes"] for p in plan)} notes in {len(plan)} notebooks: {args.out}')
    if args.server and not import_plan(plan, args.server, args.ebira):
        sys.exit('quiver2ebira: some imports failed')


if __name__ == '__main__':
    main()
