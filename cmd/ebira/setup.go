package main

import (
	"bufio"
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ikawaha/ebira/internal/config"
	"github.com/ikawaha/ebira/internal/gitsync"
	"github.com/ikawaha/ebira/internal/index"
	"github.com/ikawaha/ebira/internal/store"
	"github.com/ikawaha/ebira/internal/ui/web"
	"golang.org/x/term"
)

// defaultSetupDir is the directory setup suggests for the notes.
const defaultSetupDir = "~/notes"

// defaultSetupGitSync is the interval of syncing with git setup suggests.
const defaultSetupGitSync = "10m"

// setupFlags are flags of serve for the server started after setup: the
// language and the interval of syncing with git chosen there when they are
// not saved to the settings file.
var setupFlags []string

// setupLang is the language chosen in setup, also when it is not saved, for
// the TUI started after setup.
var setupLang string

// errNoNotes is returned when no notes directory is given and the current
// directory has no notes/.
var errNoNotes = errors.New("no notes found")

// notesDirOf returns the notes directory of a command with fs, whose -notes
// is notesDir. A notes directory given by -notes or the settings file is used
// as is, and created if needed. Otherwise notes/ in the current directory is
// used when it exists; when it does not, setup asks where to keep the notes
// if interactive, else it fails, so that notes/ is not made in a place by
// accident.
func notesDirOf(fs *flag.FlagSet, notesDir string, interactive bool, in io.Reader, out io.Writer) (string, error) {
	dir, err := absDir(notesDir)
	if err != nil {
		return "", err
	}
	if flagSet(fs, "notes") || settings.NotesDir != "" || isDir(dir) {
		return dir, nil
	}
	if !interactive {
		return "", fmt.Errorf("%w: no %s; run ebira in a terminal to set up, or pass -notes", errNoNotes, dir)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return setup(cwd, in, out)
}

// flagSet reports whether the flag name was given on fs.
func flagSet(fs *flag.FlagSet, name string) bool {
	set := false
	fs.Visit(func(f *flag.Flag) { set = set || f.Name == name })
	return set
}

// isDir reports whether dir is a directory.
func isDir(dir string) bool {
	fi, err := os.Stat(dir)
	return err == nil && fi.IsDir()
}

// hasFiles reports whether the directory dir has files other than .DS_Store.
func hasFiles(dir string) bool {
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() != ".DS_Store" {
			return true
		}
	}
	return false
}

// hasNotes reports whether dir is a notes directory of ebira, with the inbox
// or the settings shared with the notes.
func hasNotes(dir string) bool {
	if isDir(filepath.Join(dir, store.Inbox)) {
		return true
	}
	_, err := os.Stat(filepath.Join(dir, store.ConfigFile))
	return err == nil
}

// interactive reports whether the standard input is a terminal, to ask the
// user.
func interactive() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// setup asks where to keep the notes, with the current directory cwd lacking
// them, whether to sync them with a git repository and how often, how to
// search them and, unless the settings file has it, the language of the web
// UI; it creates the notes directory, cloning the notes from the repository
// when it has some, else pushing them to it, and saves the directory, the
// language and the interval to the settings file if the user agrees.
func setup(cwd string, in io.Reader, out io.Writer) (string, error) {
	ctx := context.Background()
	r := bufio.NewReader(in)
	cfgPath, err := config.Path()
	if err != nil {
		return "", err
	}
	fmt.Fprintf(out, "No notes found: no -notes, no notes_dir in %s, and no notes/ in %s.\nLet's set up ebira.\n\n", cfgPath, cwd)

	dir, err := askDir(r, out, cwd)
	if err != nil {
		return "", err
	}

	url, remoteNotes := "", false
	if svc := syncedFolder(dir); svc != "" {
		fmt.Fprintf(out, "\nThe notes are in %s, which syncs them among machines. When a note is changed on two\nmachines, ebira makes the copy %s leaves into a note, titled as a conflict copy.\nGit is not used: a git repository in a synced folder can break.\n", svc, svc)
	} else if url, remoteNotes, err = askRemote(ctx, r, out, dir); err != nil {
		return "", err
	}

	mode := ""
	switch {
	case hasNotes(dir):
		fmt.Fprintf(out, "Found notes in %s; its settings are kept.\n", dir)
	case remoteNotes:
		fmt.Fprintf(out, "Found notes in %s; they are cloned, with their settings.\n", url)
	default:
		fmt.Fprintln(out, "\nSearch mode: ja analyzes Japanese into words, so that a search finds other forms of a word,\nsuch as the past tense of a verb, and also matches characters as bigram does.\nbigram only matches characters; it is enough for notes mostly in English.")
		ja, err := confirm(r, out, "Use ja", true)
		if err != nil {
			return "", err
		}
		mode = string(index.ModeBigram)
		if ja {
			mode = string(index.ModeJa)
		}
	}
	gitSync := ""
	if url != "" {
		fmt.Fprintln(out)
		if gitSync, err = askGitSync(r, out); err != nil {
			return "", err
		}
	}
	lang := ""
	if settings.Lang == "" {
		fmt.Fprintln(out)
		if lang, err = askLang(r, out); err != nil {
			return "", err
		}
		setupLang = lang
	}
	fmt.Fprintln(out)
	what := "the directory as notes_dir"
	if lang != "" {
		what += ", the language as lang"
	}
	if gitSync != "" {
		what += ", the interval of syncing as git_sync"
	}
	save, err := confirm(r, out, fmt.Sprintf("Save %s in %s, to use them from any directory", what, cfgPath), true)
	if err != nil {
		return "", err
	}

	fmt.Fprintf(out, "\nNotes:        %s\n", dir)
	switch {
	case url == "":
	case remoteNotes && !hasFiles(dir):
		fmt.Fprintf(out, "Git:          clone %s\n", url)
	default:
		fmt.Fprintf(out, "Git:          push to %s\n", url)
	}
	if gitSync != "" {
		fmt.Fprintf(out, "Sync:         every %s\n", gitSync)
	}
	if mode != "" {
		fmt.Fprintf(out, "Search mode:  %s (in %s)\n", mode, store.ConfigFile)
	}
	if lang != "" {
		fmt.Fprintf(out, "Web UI:       %s\n", lang)
	}
	if save {
		fmt.Fprintf(out, "Settings:     saved in %s\n", cfgPath)
	}
	if ok, err := confirm(r, out, "Set up so", true); err != nil {
		return "", err
	} else if !ok {
		return "", errors.New("setup canceled")
	}

	if err := makeNotes(ctx, dir, url, remoteNotes, mode, out); err != nil {
		return "", err
	}
	if save {
		if _, err := config.Save(config.Config{NotesDir: dir, Lang: lang, GitSync: gitSync}); err != nil {
			return "", err
		}
		settings.NotesDir = dir
		settings.Lang = cmp.Or(lang, settings.Lang)
		settings.GitSync = cmp.Or(gitSync, settings.GitSync)
	} else {
		if lang != "" {
			setupFlags = append(setupFlags, "-lang", lang)
		}
		if gitSync != "" {
			setupFlags = append(setupFlags, "-git-sync", gitSync)
		}
	}
	fmt.Fprintln(out)
	return dir, nil
}

// makeNotes creates the notes directory dir: it clones the notes from the git
// repository url when it has notes (remoteNotes) and dir has no files,
// else creates them with the search mode, unless empty, and pushes them to
// url, unless empty. A directory dir made here is removed on a failure.
func makeNotes(ctx context.Context, dir, url string, remoteNotes bool, mode string, out io.Writer) (err error) {
	if _, statErr := os.Stat(dir); errors.Is(statErr, fs.ErrNotExist) {
		defer func() {
			if err != nil {
				os.RemoveAll(dir)
			}
		}()
	}
	if url != "" && remoteNotes && !hasFiles(dir) {
		fmt.Fprintf(out, "Cloning %s...\n", url)
		return gitsync.Clone(ctx, url, dir)
	}
	if mode != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, store.ConfigFile), []byte("search: "+mode+"\n"), 0o644); err != nil {
			return err
		}
	}
	if url == "" {
		return nil
	}
	st, err := store.New(dir)
	if err != nil {
		return err
	}
	label := "conflict " + time.Now().Format("2006-01-02 15:04")
	fmt.Fprintf(out, "Pushing the notes to %s...\n", url)
	_, err = gitsync.Connect(ctx, dir, url, func(f func() error) error { return f() }, store.IsNotePath,
		func(p string, theirs []byte) (string, error) { return st.ConflictCopy(p, theirs, label) })
	return err
}

// syncedFolder returns the name of the file sync service, such as Dropbox or
// iCloud Drive, whose folder holds dir, or "" for none known.
func syncedFolder(dir string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	// Resolve the links of the part that exists, such as ~/Dropbox to
	// ~/Library/CloudStorage/Dropbox.
	p := dir
	var rest []string
	for {
		if real, err := filepath.EvalSymlinks(p); err == nil {
			p = filepath.Join(append([]string{real}, rest...)...)
			break
		}
		parent := filepath.Dir(p)
		if parent == p {
			p = dir
			break
		}
		rest = append([]string{filepath.Base(p)}, rest...)
		p = parent
	}
	if realHome, err := filepath.EvalSymlinks(home); err == nil {
		home = realHome
	}
	rel, err := filepath.Rel(home, p)
	if err != nil || strings.HasPrefix(rel, "..") {
		return ""
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	switch {
	case len(parts) >= 3 && parts[0] == "Library" && parts[1] == "CloudStorage":
		name, _, _ := strings.Cut(parts[2], "-") // such as GoogleDrive-you@example.com
		if name == "GoogleDrive" {
			return "Google Drive"
		}
		return name
	case len(parts) >= 2 && parts[0] == "Library" && parts[1] == "Mobile Documents":
		return "iCloud Drive"
	case parts[0] == "Dropbox" || strings.HasPrefix(parts[0], "Dropbox ("):
		return "Dropbox"
	case strings.HasPrefix(parts[0], "OneDrive"):
		return "OneDrive"
	case parts[0] == "Google Drive":
		return "Google Drive"
	}
	return ""
}

// askDir asks for the notes directory, relative to cwd. A directory with
// files that are not notes of ebira, such as notes of another app, is
// confirmed, as ebira takes the folders in it for notebooks.
func askDir(r *bufio.Reader, out io.Writer, cwd string) (string, error) {
	for {
		answer, err := ask(r, out, "Directory to keep the notes in", defaultSetupDir)
		if err != nil {
			return "", err
		}
		dir, err := expandDir(answer, cwd)
		if err != nil || !hasFiles(dir) || hasNotes(dir) {
			return dir, err
		}
		fmt.Fprintf(out, "%s has files, but no notes of ebira. ebira takes the folders in it for notebooks\nand its YAML files for notes; other files are left as they are.\n", dir)
		if ok, err := confirm(r, out, "Keep the notes there", false); err != nil || ok {
			return dir, err
		}
	}
}

// askRemote asks for the URL of a git repository to sync the notes
// directory notes with, unless notes is in one already or git is not
// installed, and reports whether the repository has notes. An empty answer
// is none.
func askRemote(ctx context.Context, r *bufio.Reader, out io.Writer, notes string) (url string, hasNotes bool, err error) {
	if _, err := exec.LookPath("git"); err != nil {
		return "", false, nil
	}
	if _, err := gitsync.Open(ctx, notes); err == nil {
		return "", false, nil
	}
	fmt.Fprintln(out, "\nGit: ebira can sync the notes with a git repository, such as a private one on GitHub,\nto share them among machines. Give the URL of an empty repository, or of one with notes\nfrom another machine.")
	for {
		url, err := ask(r, out, "URL of the repository, or empty for none", "")
		if err != nil || url == "" {
			return "", false, err
		}
		b, err := gitsync.RemoteBranch(ctx, url)
		if err == nil {
			return url, b != "", nil
		}
		fmt.Fprintf(out, "Cannot read the repository: %v\nCheck the URL and that git can reach it without a password, such as with an SSH key.\n", err)
	}
}

// askGitSync asks how often to sync with git: an interval, or "" for never.
func askGitSync(r *bufio.Reader, out io.Writer) (string, error) {
	for {
		answer, err := ask(r, out, "Sync with git automatically every (0 for never; ebira sync syncs by hand)", defaultSetupGitSync)
		if err != nil {
			return "", err
		}
		d, err := gitSyncInterval(answer)
		if err == nil {
			if d == 0 {
				return "", nil
			}
			return answer, nil
		}
		fmt.Fprintf(out, "Answer a duration of at least %s, such as 10m or 1h, or 0.\n", minGitSync)
	}
}

// askLang asks for the language of the UI, the web UI and the TUI. The
// default is ja when $LANG is Japanese, else en.
func askLang(r *bufio.Reader, out io.Writer) (string, error) {
	def := "en"
	if strings.HasPrefix(os.Getenv("LANG"), "ja") {
		def = "ja"
	}
	for {
		answer, err := ask(r, out, "Language of the web UI and the TUI ("+strings.Join(web.Langs, "/")+")", def)
		if err != nil {
			return "", err
		}
		if l := strings.ToLower(answer); web.ValidLang(l) {
			return l, nil
		}
		fmt.Fprintf(out, "Answer one of %s.\n", strings.Join(web.Langs, ", "))
	}
}

// ask asks for a line, with def for an empty answer.
func ask(r *bufio.Reader, out io.Writer, prompt, def string) (string, error) {
	if def == "" {
		fmt.Fprintf(out, "%s: ", prompt)
	} else {
		fmt.Fprintf(out, "%s [%s]: ", prompt, def)
	}
	line, err := r.ReadString('\n')
	if err != nil && (!errors.Is(err, io.EOF) || line == "") {
		return "", fmt.Errorf("setup canceled: %w", err)
	}
	if s := strings.TrimSpace(line); s != "" {
		return s, nil
	}
	return def, nil
}

// confirm asks a yes or no question, with def for an empty answer.
func confirm(r *bufio.Reader, out io.Writer, prompt string, def bool) (bool, error) {
	hint := "y/N"
	if def {
		hint = "Y/n"
	}
	for {
		fmt.Fprintf(out, "%s? [%s] ", prompt, hint)
		line, err := r.ReadString('\n')
		if err != nil && (!errors.Is(err, io.EOF) || line == "") {
			return false, fmt.Errorf("setup canceled: %w", err)
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "":
			return def, nil
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		}
		fmt.Fprintln(out, "Answer y or n.")
	}
}

// expandDir expands a leading ~ of path and makes it absolute against cwd.
func expandDir(path, cwd string) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, path[1:])
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	return filepath.Clean(path), nil
}
