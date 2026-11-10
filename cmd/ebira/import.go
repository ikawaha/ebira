package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"mime/multipart"
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
		notesDir = fs.String("notes", defaultNotesDir(), "notes directory whose server is used, started in the background if needed")
		addr     = fs.String("addr", defaultAddr(), "address of a server started for the import")
		server   = fs.String("server", "", "URL of a server to use as is, without starting one, such as http://localhost:13579")
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
			dir, err := notesDirOf(fs, *notesDir, interactive(), os.Stdin, os.Stdout)
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
		hc, err := signedInClient(ctx, u)
		if err != nil {
			return err
		}
		send = notescli.NewClient(u.Scheme, u.Host, hc, goahttp.RequestEncoder, goahttp.ResponseDecoder, false).Import()
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
		src, err := readImport(f)
		if err != nil {
			fmt.Fprintf(out, "failed   %s: %v\n", f, err)
			failed++
			continue
		}
		if send == nil {
			fmt.Fprintf(out, "would import  %s  → %q", f, src.note.Title)
			if len(src.note.Tags) > 0 {
				fmt.Fprintf(out, "  tags: %s", strings.Join(src.note.Tags, ", "))
			}
			fmt.Fprintf(out, "  images: %d of %d\n", len(src.images), len(src.note.Images))
			for _, w := range src.note.Warnings {
				fmt.Fprintf(out, "    %s\n", w)
			}
			continue
		}
		res, err := src.send(ctx, send, notebook, force)
		if err != nil {
			fmt.Fprintf(out, "failed   %s: %v\n", f, err)
			failed++
			continue
		}
		switch res.Status {
		case "skipped":
			fmt.Fprintf(out, "skipped  %s  (imported before as %s)\n", f, deref(res.DuplicateOf))
			skipped++
		default:
			fmt.Fprintf(out, "created  %s  → %q (%s/%s, %d images)\n", f, res.Note.Title, res.Note.Notebook, res.Note.ID, len(src.images))
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

// importSource is a Markdown file to import and the images it refers to.
type importSource struct {
	name, content, modTime string
	note                   *importer.Note
	images                 []importImage
}

// importImage is an image a Markdown file refers to: the path as the file
// refers to it and the file on disk.
type importImage struct {
	ref, path string
}

// readImport reads the Markdown file f and finds the images it refers to by
// relative paths. Images that are not there or too large are left out; the
// server tells which links stay as they are.
func readImport(f string) (*importSource, error) {
	b, err := os.ReadFile(f)
	if err != nil {
		return nil, err
	}
	if !utf8.Valid(b) {
		return nil, importer.ErrNotUTF8
	}
	fi, err := os.Stat(f)
	if err != nil {
		return nil, err
	}
	n, err := importer.Parse(importer.Input{Name: filepath.Base(f), Content: string(b), ModTime: fi.ModTime(), Now: time.Now()})
	if err != nil {
		return nil, err
	}
	src := &importSource{name: filepath.Base(f), content: string(b), modTime: fi.ModTime().UTC().Format(time.RFC3339), note: n}
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
		if !slices.ContainsFunc(src.images, func(x importImage) bool { return x.ref == ref }) {
			src.images = append(src.images, importImage{ref: ref, path: img})
		}
	}
	return src, nil
}

// send imports the file with send, streaming the images from disk in a body
// of multipart/form-data, so that they are not held in memory.
func (src *importSource) send(ctx context.Context, send func(context.Context, any) (any, error), notebook string, force bool) (*gennotes.ImportResult, error) {
	pr, pw := io.Pipe()
	defer pr.Close() // ends the writing when the request ends early
	w := multipart.NewWriter(pw)
	go func() { pw.CloseWithError(src.write(w)) }()
	ct := w.FormDataContentType()
	p := &gennotes.ImportPayload{ContentType: &ct}
	if notebook != "" {
		p.Notebook = &notebook
	}
	if force {
		p.Force = &force
	}
	v, err := send(ctx, &gennotes.ImportRequestData{Payload: p, Body: pr})
	if err != nil {
		return nil, err
	}
	return v.(*gennotes.ImportResult), nil
}

// write writes the body of the import to w: the fields of the file, then a
// field ref and a file field for each image. An image that cannot be opened
// is left out.
func (src *importSource) write(w *multipart.Writer) error {
	for _, f := range [][2]string{{"name", src.name}, {"content", src.content}, {"modified_at", src.modTime}} {
		if err := w.WriteField(f[0], f[1]); err != nil {
			return err
		}
	}
	for _, img := range src.images {
		if err := writeImage(w, img); err != nil {
			return err
		}
	}
	return w.Close()
}

func writeImage(w *multipart.Writer, img importImage) error {
	f, err := os.Open(img.path)
	if err != nil {
		return nil // left out; the server tells the link stays
	}
	defer f.Close()
	if err := w.WriteField("ref", img.ref); err != nil {
		return err
	}
	fw, err := w.CreateFormFile("file", filepath.Base(img.path))
	if err != nil {
		return err
	}
	_, err = io.Copy(fw, f)
	return err
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
