package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ikawaha/ebira/internal/config"
	"github.com/ikawaha/ebira/internal/index"
	"github.com/ikawaha/ebira/internal/store"
	"golang.org/x/term"
)

// defaultSetupDir is the directory setup suggests for the notes.
const defaultSetupDir = "~/ebira"

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
// them, and how to search them; it creates the data directory and saves it
// to the settings file if the user agrees.
func setup(cwd string, in io.Reader, out io.Writer) (string, error) {
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

	mode := ""
	if hasNotes(dir) {
		fmt.Fprintf(out, "Found notes in %s; its settings are kept.\n", dir)
	} else {
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
	fmt.Fprintln(out)
	save, err := confirm(r, out, fmt.Sprintf("Save the directory as data_dir in %s, to use it from any directory", cfgPath), true)
	if err != nil {
		return "", err
	}

	fmt.Fprintf(out, "\nNotes:        %s\n", filepath.Join(dir, "notes"))
	if mode != "" {
		fmt.Fprintf(out, "Search mode:  %s (in notes/%s)\n", mode, store.ConfigFile)
	}
	if save {
		fmt.Fprintf(out, "Settings:     data_dir in %s\n", cfgPath)
	}
	if ok, err := confirm(r, out, "Set up so", true); err != nil {
		return "", err
	} else if !ok {
		return "", errors.New("setup canceled")
	}

	if mode != "" {
		notes := filepath.Join(dir, "notes")
		if err := os.MkdirAll(notes, 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(notes, store.ConfigFile), []byte("search: "+mode+"\n"), 0o644); err != nil {
			return "", err
		}
	}
	if save {
		if _, err := config.SaveDataDir(dir); err != nil {
			return "", err
		}
		settings.DataDir = dir
	}
	fmt.Fprintln(out)
	return dir, nil
}

// ask asks for a line, with def for an empty answer.
func ask(r *bufio.Reader, out io.Writer, prompt, def string) (string, error) {
	fmt.Fprintf(out, "%s [%s]: ", prompt, def)
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
