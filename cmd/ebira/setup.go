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
const defaultSetupDir = "~/ebira"

// defaultSetupGitSync is the interval of syncing with git setup suggests.
const defaultSetupGitSync = "10m"

// setupFlags are flags of serve for the server started after setup: the
// language and the interval of syncing with git chosen there when they are
// not saved to the settings file.
var setupFlags []string

// errNoNotes is returned when no data directory is given and the current
// directory has no notes.
var errNoNotes = errors.New("no notes found")

// dataDirOf returns the data directory of a command with fs, whose -data-dir
// is dataDir. A data directory given by -data-dir or the settings file is
// used as is, and created if needed. Otherwise the current directory is used
// when it has notes; when it has none, setup asks where to keep them if
// interactive, else it fails, so that notes/ is not made in a place by
// accident.
func dataDirOf(fs *flag.FlagSet, dataDir string, interactive bool, in io.Reader, out io.Writer) (string, error) {
	dir, err := absDir(dataDir)
	if err != nil {
		return "", err
	}
	if flagSet(fs, "data-dir") || settings.DataDir != "" || hasNotes(dir) {
		return dir, nil
	}
	if !interactive {
		return "", fmt.Errorf("%w in %s (no notes/ directory); run ebira in a terminal to set up, or pass -data-dir", errNoNotes, dir)
	}
	return setup(dir, in, out)
}

// flagSet reports whether the flag name was given on fs.
func flagSet(fs *flag.FlagSet, name string) bool {
	set := false
	fs.Visit(func(f *flag.Flag) { set = set || f.Name == name })
	return set
}

// hasNotes reports whether dir is a data directory, with notes/ in it.
func hasNotes(dir string) bool {
	fi, err := os.Stat(filepath.Join(dir, "notes"))
	return err == nil && fi.IsDir()
}

// interactive reports whether the standard input is a terminal, to ask the
// user.
func interactive() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// setup asks where to keep the notes, with the current directory cwd lacking
// them, whether to sync them with a git repository and how often, how to
// search them and, unless the settings file has it, the language of the web
// UI; it creates the data directory, cloning the notes from the repository
// when it has some, else pushing them to it, and saves the directory, the
// language and the interval to the settings file if the user agrees.
func setup(cwd string, in io.Reader, out io.Writer) (string, error) {
	ctx := context.Background()
	r := bufio.NewReader(in)
	cfgPath, err := config.Path()
	if err != nil {
		return "", err
	}
	fmt.Fprintf(out, "No notes found: no -data-dir, no data_dir in %s, and no notes/ in %s.\nLet's set up ebira.\n\n", cfgPath, cwd)

	answer, err := ask(r, out, "Directory to keep the notes in", defaultSetupDir)
	if err != nil {
		return "", err
	}
	dir, err := expandDir(answer, cwd)
	if err != nil {
		return "", err
	}

	notes := filepath.Join(dir, "notes")
	url, remoteNotes, err := askRemote(ctx, r, out, notes)
	if err != nil {
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
	}
	fmt.Fprintln(out)
	what := "the directory as data_dir"
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

	fmt.Fprintf(out, "\nNotes:        %s\n", filepath.Join(dir, "notes"))
	switch {
	case url == "":
	case remoteNotes && !hasNotes(dir):
		fmt.Fprintf(out, "Git:          clone %s\n", url)
	default:
		fmt.Fprintf(out, "Git:          push to %s\n", url)
	}
	if gitSync != "" {
		fmt.Fprintf(out, "Sync:         every %s\n", gitSync)
	}
	if mode != "" {
		fmt.Fprintf(out, "Search mode:  %s (in notes/%s)\n", mode, store.ConfigFile)
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
		if _, err := config.Save(config.Config{DataDir: dir, Lang: lang, GitSync: gitSync}); err != nil {
			return "", err
		}
		settings.DataDir = dir
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

// makeNotes creates the notes of the data directory dir: it clones them from
// the git repository url when it has notes (remoteNotes) and dir has none,
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
	notes := filepath.Join(dir, "notes")
	if url != "" && remoteNotes && !hasNotes(dir) {
		fmt.Fprintf(out, "Cloning %s...\n", url)
		return gitsync.Clone(ctx, url, notes)
	}
	if mode != "" {
		if err := os.MkdirAll(notes, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(notes, store.ConfigFile), []byte("search: "+mode+"\n"), 0o644); err != nil {
			return err
		}
	}
	if url == "" {
		return nil
	}
	st, err := store.New(notes)
	if err != nil {
		return err
	}
	label := "conflict " + time.Now().Format("2006-01-02 15:04")
	fmt.Fprintf(out, "Pushing the notes to %s...\n", url)
	_, err = gitsync.Connect(ctx, notes, url, func(f func() error) error { return f() }, store.IsNotePath,
		func(p string, theirs []byte) (string, error) { return st.ConflictCopy(p, theirs, label) })
	return err
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

// askLang asks for the language of the web UI. The default is ja when $LANG
// is Japanese, else en.
func askLang(r *bufio.Reader, out io.Writer) (string, error) {
	def := "en"
	if strings.HasPrefix(os.Getenv("LANG"), "ja") {
		def = "ja"
	}
	for {
		answer, err := ask(r, out, "Language of the web UI ("+strings.Join(web.Langs, "/")+")", def)
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
