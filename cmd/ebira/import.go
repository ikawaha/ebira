package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	notescli "github.com/ikawaha/ebira/gen/http/notes/client"
	gennotes "github.com/ikawaha/ebira/gen/notes"
	"github.com/ikawaha/ebira/internal/importer"
	"github.com/ikawaha/ebira/internal/store"
	goahttp "goa.design/goa/v3/http"
)

// runImport runs ebira import: it imports Markdown files, with the images
// they refer to by relative paths, through the server's API.
func runImport(args []string) error {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	var (
		dataDir  = fs.String("data-dir", defaultDataDir(), "data directory whose server is used, started in the background if needed")
		addr     = fs.String("addr", defaultAddr(), "address of a server started for the import")
		server   = fs.String("server", "", "URL of a server to use as is, without starting one, such as http://localhost:8080")
		notebook = fs.String("notebook", "", "notebook ID to import into (default inbox)")
		force    = fs.Bool("force", false, "import files whose content was imported before")
		dryRun   = fs.Bool("dry-run", false, "only show what would be imported, without a server")
	)
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), "usage: ebira import [flags] file or directory...\n\nImport Markdown files (.md, .markdown; directories are searched recursively) as notes.\n\nflags:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		fs.Usage()
		return errors.New("no files to import")
	}
	files, err := markdownFiles(fs.Args())
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return errors.New("no Markdown files found")
	}
	ctx := context.Background()
	var send func(context.Context, any) (any, error)
	if !*dryRun {
		base := *server
		if base == "" {
			dir, err := dataDirOf(fs, *dataDir, interactive(), os.Stdin, os.Stdout)
			if err != nil {
				return err
			}
			a, err := ensureServer(ctx, dir, *addr, os.Stdin, os.Stdout)
			if err != nil {
				return err
			}
			base = "http://" + a
		}
		u, err := url.Parse(base)
		if err != nil || u.Host == "" {
			return fmt.Errorf("invalid server URL %q", base)
		}
		send = notescli.NewClient(u.Scheme, u.Host, http.DefaultClient, goahttp.RequestEncoder, goahttp.ResponseDecoder, false).Import()
	}
	return importFiles(ctx, files, *notebook, *force, send, os.Stdout)
}

// markdownFiles returns the Markdown files among paths, searching directories
// recursively and skipping hidden ones.
func markdownFiles(paths []string) ([]string, error) {
	var files []string
	for _, p := range paths {
		fi, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		if !fi.IsDir() {
			files = append(files, p)
			continue
		}
		err = filepath.WalkDir(p, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if path != p && strings.HasPrefix(d.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			if isMarkdown(d.Name()) && !strings.HasPrefix(d.Name(), ".") {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return files, nil
}

func isMarkdown(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".md" || ext == ".markdown"
}

// importFiles imports files one by one with send, or only shows them when
// send is nil, and prints the outcome. It fails when a file failed.
func importFiles(ctx context.Context, files []string, notebook string, force bool, send func(context.Context, any) (any, error), out io.Writer) error {
	created, skipped, failed := 0, 0, 0
	for _, f := range files {
		p, n, err := importPayload(f)
		if err != nil {
			fmt.Fprintf(out, "failed   %s: %v\n", f, err)
			failed++
			continue
		}
		if notebook != "" {
			p.Notebook = &notebook
		}
		if force {
			p.Force = &force
		}
		if send == nil {
			fmt.Fprintf(out, "would import  %s  → %q", f, n.Title)
			if len(n.Tags) > 0 {
				fmt.Fprintf(out, "  tags: %s", strings.Join(n.Tags, ", "))
			}
			fmt.Fprintf(out, "  images: %d of %d\n", len(p.Files), len(n.Images))
			for _, w := range n.Warnings {
				fmt.Fprintf(out, "    %s\n", w)
			}
			continue
		}
		v, err := send(ctx, p)
		if err != nil {
			fmt.Fprintf(out, "failed   %s: %v\n", f, err)
			failed++
			continue
		}
		res := v.(*gennotes.ImportResult)
		switch res.Status {
		case "skipped":
			fmt.Fprintf(out, "skipped  %s  (imported before as %s)\n", f, deref(res.DuplicateOf))
			skipped++
		default:
			fmt.Fprintf(out, "created  %s  → %q (%s/%s, %d images)\n", f, res.Note.Title, res.Note.Notebook, res.Note.ID, len(p.Files))
			created++
		}
		for _, w := range res.Warnings {
			fmt.Fprintf(out, "    %s\n", w)
		}
	}
	if send == nil {
		fmt.Fprintf(out, "%d files (dry run)\n", len(files))
		return nil
	}
	fmt.Fprintf(out, "%d created, %d skipped, %d failed\n", created, skipped, failed)
	if failed > 0 {
		return fmt.Errorf("%d files failed", failed)
	}
	return nil
}

// importPayload reads the Markdown file f and the images it refers to by
// relative paths. Images that cannot be read are left out; the server tells
// which links stay as they are.
func importPayload(f string) (*gennotes.ImportPayload, *importer.Note, error) {
	b, err := os.ReadFile(f)
	if err != nil {
		return nil, nil, err
	}
	if !utf8.Valid(b) {
		return nil, nil, importer.ErrNotUTF8
	}
	fi, err := os.Stat(f)
	if err != nil {
		return nil, nil, err
	}
	n, err := importer.Parse(importer.Input{Name: filepath.Base(f), Content: string(b), ModTime: fi.ModTime(), Now: time.Now()})
	if err != nil {
		return nil, nil, err
	}
	mod := fi.ModTime().UTC().Format(time.RFC3339)
	p := &gennotes.ImportPayload{Name: filepath.Base(f), Content: string(b), ModifiedAt: &mod}
	for _, ref := range n.Images {
		rel, err := url.PathUnescape(ref)
		if err != nil {
			rel = ref
		}
		img := filepath.Join(filepath.Dir(f), filepath.FromSlash(rel))
		fi, err := os.Stat(img)
		if err != nil || !fi.Mode().IsRegular() || fi.Size() > store.MaxFileSize {
			continue
		}
		content, err := os.ReadFile(img)
		if err != nil {
			continue
		}
		if !slices.ContainsFunc(p.Files, func(x *gennotes.ImportFile) bool { return x.Ref == ref }) {
			p.Files = append(p.Files, &gennotes.ImportFile{Ref: ref, Name: filepath.Base(img), Content: content})
		}
	}
	return p, n, nil
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
