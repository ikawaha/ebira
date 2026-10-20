package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"time"

	notescli "github.com/ikawaha/ebira/gen/http/notes/client"
	gennotes "github.com/ikawaha/ebira/gen/notes"
	goahttp "goa.design/goa/v3/http"
)

// runFiles runs ebira files check.
func runFiles(args []string) error {
	if len(args) == 0 || args[0] != "check" {
		return errors.New("ebira files needs check")
	}
	fs := flag.NewFlagSet("files check", flag.ContinueOnError)
	var (
		notesDir = fs.String("notes", defaultNotesDir(), "notes directory whose server is used, started in the background if needed")
		addr     = fs.String("addr", defaultAddr(), "address of a server started for the check")
		server   = fs.String("server", "", "URL of a server to use as is, without starting one, such as http://localhost:13579")
		del      = fs.Bool("delete", false, "delete the unused files, except those added within the last hour")
	)
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	ctx := context.Background()
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
	c := notescli.NewClient(u.Scheme, u.Host, hc, goahttp.RequestEncoder, goahttp.ResponseDecoder, false)
	return checkFiles(ctx, c.CheckFiles(), c.DeleteFile(), *del, os.Stdout)
}

// checkFiles prints the unused files and the broken references, and deletes
// the unused files that are not recent when del is set.
func checkFiles(ctx context.Context, check, deleteFile func(context.Context, any) (any, error), del bool, out io.Writer) error {
	v, err := check(ctx, &gennotes.CheckFilesPayload{})
	if err != nil {
		return err
	}
	res := v.(*gennotes.FileCheck)
	fmt.Fprintf(out, "Unused files: %d\n", len(res.Unused))
	var size int64
	for _, f := range res.Unused {
		note := ""
		if f.Recent {
			note = "  (added within the last hour; the note may be being edited)"
		}
		added := f.AddedAt
		if t, err := time.Parse(time.RFC3339, f.AddedAt); err == nil {
			added = t.Local().Format(time.DateTime)
		}
		fmt.Fprintf(out, "  %s  %s  %s  %s%s\n", f.Path, formatSize(f.Size), added, f.Title, note)
		size += f.Size
	}
	fmt.Fprintf(out, "Broken references: %d\n", len(res.Broken))
	for _, b := range res.Broken {
		fmt.Fprintf(out, "  %s  in %s (%s)\n", b.Path, b.Title, b.NoteID)
	}
	if !del {
		if len(res.Unused) > 0 {
			fmt.Fprintf(out, "%s in unused files; run with -delete to delete them, except recent ones.\n", formatSize(size))
		}
		return nil
	}
	deleted, freed := 0, int64(0)
	for _, f := range res.Unused {
		if f.Recent {
			continue
		}
		if _, err := deleteFile(ctx, &gennotes.DeleteFilePayload{ID: f.NoteID, Name: f.Name}); err != nil {
			return fmt.Errorf("delete %s: %w", f.Path, err)
		}
		deleted++
		freed += f.Size
	}
	fmt.Fprintf(out, "Deleted %d files (%s).\n", deleted, formatSize(freed))
	return nil
}

// formatSize formats a size in bytes for people.
func formatSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}
