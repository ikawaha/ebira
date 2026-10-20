package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"

	notescli "github.com/ikawaha/ebira/gen/http/notes/client"
	gennotes "github.com/ikawaha/ebira/gen/notes"
	goahttp "goa.design/goa/v3/http"
)

// runSync runs ebira sync.
func runSync(args []string) error {
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	var (
		notesDir = fs.String("notes", defaultNotesDir(), "notes directory whose server syncs, started in the background if needed")
		addr     = fs.String("addr", defaultAddr(), "address of a server started for the sync")
		server   = fs.String("server", "", "URL of a server to use as is, without starting one, such as http://localhost:13579")
		remote   = fs.String("remote", "", "URL of a remote repository to connect the notes to first, as the remote origin: the notes directory becomes a repository unless it is one, and the notes are pushed, merging those already there")
	)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), `usage: ebira sync [flags]

Sync the notes with the git repository they are in: commit the changes of
the notes directory, then, when the branch has an upstream branch, merge it
and push. A note changed on both sides keeps this side's version, and the
other side's version becomes a new note next to it, with the conflict in its
title.

To start syncing notes not in git yet, give the URL of a remote repository
with -remote, once; an empty one, or one with notes from another machine.`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
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
	c := notescli.NewClient(u.Scheme, u.Host, http.DefaultClient, goahttp.RequestEncoder, goahttp.ResponseDecoder, false)
	p := &gennotes.GitSyncPayload{}
	if *remote != "" {
		p.Remote = remote
	}
	v, err := c.GitSync()(ctx, p)
	if err != nil {
		return err
	}
	printSync(v.(*gennotes.GitSyncResult), os.Stdout)
	return nil
}

// printSync prints the result of a sync with git.
func printSync(r *gennotes.GitSyncResult, out io.Writer) {
	fmt.Fprintf(out, "Files committed: %d\n", r.Committed)
	if r.Upstream == nil {
		fmt.Fprintln(out, "No upstream branch: the changes are only committed. Set one with git push -u <remote> <branch> in the repository.")
	} else {
		fmt.Fprintf(out, "Files merged from %s: %d\n", *r.Upstream, r.Merged)
		if r.Pushed {
			fmt.Fprintf(out, "Pushed to %s\n", *r.Upstream)
		} else {
			fmt.Fprintln(out, "Nothing to push")
		}
	}
	if len(r.Conflicts) > 0 {
		fmt.Fprintf(out, "Files changed on both sides: %d\n", len(r.Conflicts))
		for _, c := range r.Conflicts {
			switch {
			case c.CopyID != nil:
				fmt.Fprintf(out, "  %s: kept this version; the other is the new note %s\n", c.Path, *c.CopyID)
			default:
				fmt.Fprintf(out, "  %s: kept this or the changed version; the other is in the merge commit\n", c.Path)
			}
		}
	}
	for _, e := range r.Errors {
		fmt.Fprintf(out, "Cannot read: %s\n", e)
	}
}
