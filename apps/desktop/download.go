package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// The files of notes are saved in the downloads folder. The page reads a file
// with its own session, which a browser opened by the app lacks under passkey
// sign-in, and sends it in parts of base64 (downloadFile of the Web UI); the
// app saves it when the last part comes and tells the page (window.ebira.
// downloaded).

// maxDownload is the most a file may hold, above the largest file of a note.
const maxDownload = 64 << 20

// downloads holds the files the page is sending, by the ID the page gave.
type downloads struct {
	mu    sync.Mutex
	files map[string][]byte
}

// add adds part, in base64, to the file id, and returns its content when end
// says it was the last part.
func (d *downloads) add(id, part string, end bool) ([]byte, error) {
	b, err := base64.StdEncoding.DecodeString(part)
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.files == nil {
		d.files = map[string][]byte{}
	}
	data := append(d.files[id], b...)
	switch {
	case err != nil:
		delete(d.files, id)
		return nil, err
	case len(data) > maxDownload:
		delete(d.files, id)
		return nil, fmt.Errorf("larger than %d MB", maxDownload>>20)
	case end:
		delete(d.files, id)
		return data, nil
	}
	d.files[id] = data
	return nil, nil
}

// download takes a part of a file from the page, and saves the file when it
// is the last.
func (s *shell) download(id, name, part string, end bool) {
	data, err := s.downloads.add(id, part, end)
	if err == nil && !end {
		return
	}
	saved := ""
	if err == nil {
		saved, err = saveDownload(name, data)
	}
	if err != nil {
		log.Print("save a file: ", err)
		s.call("downloaded", name, err.Error())
		return
	}
	s.call("downloaded", saved, "")
}

// saveDownload writes data to the downloads folder as name, or as name with a
// number, such as "a 2.pdf", when it is taken, and returns the name it took.
func saveDownload(name string, data []byte) (string, error) {
	name = filepath.Base(strings.ReplaceAll(name, `\`, "/"))
	if name == "." || name == "/" || name == ".." || strings.HasPrefix(name, ".") {
		return "", fmt.Errorf("not a name of a file: %q", name)
	}
	dir, err := downloadsDir()
	if err != nil {
		return "", err
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 1; ; i++ {
		try := name
		if i > 1 {
			try = fmt.Sprintf("%s %d%s", stem, i, ext)
		}
		path := filepath.Join(dir, try)
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, err = f.Write(data)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			os.Remove(path)
			return "", err
		}
		// As a browser does: a file from a note, which AI may have added, is
		// checked by the system when it is opened.
		if err := markDownloaded(path); err != nil {
			log.Print("mark a saved file: ", err)
		}
		return try, nil
	}
}
