// Package board keeps the whiteboards: a text to understand (the source),
// such as what Claude Code wrote, with sticky notes on the passages a person
// asked about, answered by the assistant, until the person is done with it.
//
// A board lives in the state directory of the notes directory, as
// <dir>/<board ID>/, and is not synced with the notes: it changes with every
// sticky note, and becomes a note only when the person chooses so at the end.
// The source is in source.md, a copy taken when the board was opened that never
// changes, so that the anchors of the sticky notes stay where they were; the
// rest is in board.json.
package board

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/ikawaha/ebira/internal/store"
)

// Limits on boards.
const (
	// MaxSource is the largest source, in bytes.
	MaxSource = 2 << 20
	// MaxOpen is the most boards open at once.
	MaxOpen = 50
	// MaxStickies is the most sticky notes on a board.
	MaxStickies = 200
	// MaxMessage is the longest message of the person, in bytes.
	MaxMessage = 10 << 10
	// MaxAnswer is the longest answer of the assistant, in bytes; a longer one
	// is cut.
	MaxAnswer = 32 << 10
	// MaxMessages is the most messages of a sticky note.
	MaxMessages = 50
	// MaxQuote is the longest passage a sticky note is anchored to, in bytes.
	MaxQuote = 10 << 10
	// MaxContext is the longest text around the passage kept in an anchor,
	// in characters.
	MaxContext = 256
	// MaxTitle is the longest title, in characters.
	MaxTitle = 200
	// MaxResult is the longest text given back to the session, in bytes.
	MaxResult = 64 << 10

	// DoneAge is how long a finished board is kept, for the session to read
	// what is given back to it.
	DoneAge = 7 * 24 * time.Hour
	// IdleAge is how long an open board is kept untouched.
	IdleAge = 30 * 24 * time.Hour
)

// Errors of boards. The others are about invalid input, wrapping ErrInvalid.
var (
	ErrNotFound = errors.New("board not found")
	ErrInvalid  = errors.New("invalid board")
	ErrTooLarge = errors.New("too large")
	ErrTooMany  = errors.New("too many")
	ErrDone     = errors.New("the board is finished")
)

// Statuses of boards.
const (
	Open = "open"
	Done = "done"
)

// Where the source of a board comes from.
const (
	FromUpload = "upload" // sent to a URL given to an AI client
	FromText   = "text"   // given in the request
	FromNote   = "note"   // the body of a note
)

// Formats of the source.
const (
	Markdown = "markdown"
	Text     = "text"
	Diff     = "diff"
	Code     = "code"
)

// Kinds of sticky notes.
const (
	Memo      = "memo"      // written by the person
	Assistant = "assistant" // answered by the assistant
	Fork      = "fork"      // answered by a fork of the Claude Code session that opened the board
)

// States of sticky notes.
const (
	Idle    = "idle"    // nothing to wait for
	Waiting = "waiting" // an answer is coming
	Failed  = "error"   // the last answer failed; see Sticky.Error
)

// Roles of messages.
const (
	User  = "user"
	Agent = "assistant"
)

// Board is a whiteboard.
type Board struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Status    string    `json:"status"`
	Source    Source    `json:"source"`
	Origin    Origin    `json:"origin,omitzero"`
	Stickies  []Sticky  `json:"stickies"`
	Result    *Result   `json:"result,omitempty"`
	// Opened is the digest of the sticky notes when a person last opened
	// the board from its note (see StickiesDigest), to tell whether they
	// changed since.
	Opened string `json:"opened,omitempty"`
}

// StickiesDigest returns a digest of the sticky notes of b, with their
// messages and states.
func (b *Board) StickiesDigest() string {
	stickies := b.Stickies
	if stickies == nil {
		stickies = []Sticky{}
	}
	data, err := json.Marshal(stickies)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Unchanged tells whether the sticky notes of b are as they were when a
// person last opened it from its note.
func (b *Board) Unchanged() bool {
	return b.Opened != "" && b.Opened == b.StickiesDigest()
}

// Source tells where the source came from and how to show it.
type Source struct {
	From   string `json:"from"`
	NoteID string `json:"note_id,omitempty"`
	// NoteVersion is the version of the note when the board was opened from
	// it, to tell whether it changed since.
	NoteVersion string `json:"note_version,omitempty"`
	Format      string `json:"format"`
	Lang        string `json:"lang,omitempty"`
	// Size is the size of source.md, in bytes.
	Size int `json:"size"`
}

// Origin tells who opened the board: an AI client, waiting for what is given
// back, and the Claude Code session the sticky notes fork: that of Claude
// Code when it opened the board, or one ebira made to read the source
// (Seeded), which goes with the board.
type Origin struct {
	AI        bool   `json:"ai,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	Cwd       string `json:"cwd,omitempty"`
	Seeded    bool   `json:"seeded,omitempty"`
}

// Sticky is a sticky note on a passage of the source, or of the answer of
// another sticky note (its parent).
type Sticky struct {
	ID       string    `json:"id"`
	Parent   string    `json:"parent,omitempty"`
	Anchor   Anchor    `json:"anchor"`
	Kind     string    `json:"kind"`
	Messages []Message `json:"messages"`
	State    string    `json:"state"`
	Error    string    `json:"error,omitempty"`
	// Session is the Claude Code session that answers a sticky note of kind
	// fork: a fork of the session of the board, or of that of its parent.
	Session   string    `json:"session,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Anchor is the passage a sticky note is on, as a W3C text quote selector:
// the passage, with some text before and after it to tell it apart from the
// same text elsewhere. It is found again in the rendered text, as the passage
// is chosen there and has no offset in the Markdown.
type Anchor struct {
	Exact  string `json:"exact"`
	Prefix string `json:"prefix,omitempty"`
	Suffix string `json:"suffix,omitempty"`
}

// Message is a message of a sticky note.
type Message struct {
	Role string    `json:"role"`
	Text string    `json:"text"`
	At   time.Time `json:"at"`
}

// Result is what the person gives back to the session, at the end.
type Result struct {
	Text string `json:"text"`
	// Stickies are the IDs of the sticky notes given back with Text.
	Stickies []string `json:"stickies,omitempty"`
	NoteID   string   `json:"note_id,omitempty"`
	// Appended tells whether the next points were added to the note the
	// board was opened from.
	Appended   bool      `json:"appended,omitempty"`
	FinishedAt time.Time `json:"finished_at"`
}

var (
	idPattern      = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
	langPattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9+#._-]{0,31}$`)
	sessionPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// Check checks what a board is opened with: its title, source and origin.
func (b *Board) Check() error {
	b.Title = strings.TrimSpace(b.Title)
	if b.Title == "" {
		return invalid("the title is empty")
	}
	if utf8.RuneCountInString(b.Title) > MaxTitle {
		return invalid("the title is longer than %d characters", MaxTitle)
	}
	switch b.Source.From {
	case FromUpload, FromText:
		b.Source.NoteID = ""
	case FromNote:
		if !store.ValidID(b.Source.NoteID) {
			return invalid("invalid note ID %q", b.Source.NoteID)
		}
	default:
		return invalid("unknown source %q", b.Source.From)
	}
	switch b.Source.Format {
	case "":
		b.Source.Format = Markdown
	case Markdown, Text, Diff, Code:
	default:
		return invalid("unknown format %q", b.Source.Format)
	}
	if b.Source.Lang != "" && !langPattern.MatchString(b.Source.Lang) {
		return invalid("invalid language %q", b.Source.Lang)
	}
	if s := b.Origin.SessionID; s != "" && !sessionPattern.MatchString(s) {
		return invalid("invalid session ID %q", s)
	}
	if c := b.Origin.Cwd; c != "" && (!filepath.IsAbs(c) || filepath.Clean(c) != c) {
		return invalid("the working directory %q is not a clean absolute path", c)
	}
	return nil
}

// Find returns the sticky note id of b, or nil.
func (b *Board) Find(id string) *Sticky {
	for i := range b.Stickies {
		if b.Stickies[i].ID == id {
			return &b.Stickies[i]
		}
	}
	return nil
}

// Add adds s to b, with a new ID, and returns it. The first message of s, if
// any, is the person's: a memo, or a question to the assistant.
func (b *Board) Add(s Sticky, now time.Time) (*Sticky, error) {
	if b.Status != Open {
		return nil, ErrDone
	}
	if len(b.Stickies) >= MaxStickies {
		return nil, fmt.Errorf("%w: a board has at most %d sticky notes", ErrTooMany, MaxStickies)
	}
	if s.Parent != "" && b.Find(s.Parent) == nil {
		return nil, invalid("no sticky note %q to put a sticky note on", s.Parent)
	}
	switch s.Kind {
	case Memo, Assistant, Fork:
	default:
		return nil, invalid("unknown kind of sticky note %q", s.Kind)
	}
	if err := s.Anchor.check(); err != nil {
		return nil, err
	}
	for _, m := range s.Messages {
		if err := checkMessage(m.Text); err != nil {
			return nil, err
		}
	}
	s.ID = b.newStickyID()
	s.State = Idle
	s.Error = ""
	s.CreatedAt, s.UpdatedAt = now, now
	for i := range s.Messages {
		s.Messages[i].Role = User
		s.Messages[i].At = now
	}
	b.Stickies = append(b.Stickies, s)
	return &b.Stickies[len(b.Stickies)-1], nil
}

func (b *Board) newStickyID() string {
	for {
		id := "s" + store.NewID()[:8]
		if b.Find(id) == nil {
			return id
		}
	}
}

// Remove removes the sticky note id of b, with the sticky notes on it, and
// returns them, or nil without one.
func (b *Board) Remove(id string) []Sticky {
	if b.Find(id) == nil {
		return nil
	}
	gone := map[string]bool{id: true}
	for changed := true; changed; {
		changed = false
		for _, s := range b.Stickies {
			if !gone[s.ID] && gone[s.Parent] {
				gone[s.ID], changed = true, true
			}
		}
	}
	var removed []Sticky
	b.Stickies = slices.DeleteFunc(b.Stickies, func(s Sticky) bool {
		if gone[s.ID] {
			removed = append(removed, s)
		}
		return gone[s.ID]
	})
	return removed
}

// Sessions returns the Claude Code sessions of the sticky notes in stickies,
// which ebira made for them.
func Sessions(stickies []Sticky) []string {
	var ids []string
	for _, s := range stickies {
		if s.Session != "" {
			ids = append(ids, s.Session)
		}
	}
	return ids
}

// Say adds a message to the sticky note s: one of the person, or an answer of
// the assistant, cut to MaxAnswer.
func (s *Sticky) Say(role, text string, now time.Time) error {
	if role == Agent {
		if strings.TrimSpace(text) == "" {
			return invalid("the answer is empty")
		}
		text = cut(text, MaxAnswer)
	} else if err := checkMessage(text); err != nil {
		return err
	}
	if len(s.Messages) >= MaxMessages {
		return fmt.Errorf("%w: a sticky note has at most %d messages", ErrTooMany, MaxMessages)
	}
	s.Messages = append(s.Messages, Message{Role: role, Text: text, At: now})
	s.UpdatedAt = now
	return nil
}

// cut cuts s to at most n bytes, at a character boundary.
func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func checkMessage(text string) error {
	if strings.TrimSpace(text) == "" {
		return invalid("the message is empty")
	}
	if len(text) > MaxMessage {
		return fmt.Errorf("%w: a message is at most %d bytes", ErrTooLarge, MaxMessage)
	}
	return nil
}

func (a *Anchor) check() error {
	if strings.TrimSpace(a.Exact) == "" {
		return invalid("the passage of the sticky note is empty")
	}
	if len(a.Exact) > MaxQuote {
		return fmt.Errorf("%w: the passage of a sticky note is at most %d bytes", ErrTooLarge, MaxQuote)
	}
	if utf8.RuneCountInString(a.Prefix) > MaxContext || utf8.RuneCountInString(a.Suffix) > MaxContext {
		return invalid("the text around the passage is longer than %d characters", MaxContext)
	}
	return nil
}

// Store keeps boards under a directory.
type Store struct {
	dir string
	mu  sync.Mutex
	now func() time.Time
}

// New returns a store of boards under dir, which is created if needed.
func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Store{dir: dir, now: time.Now}, nil
}

// Now returns the time to stamp boards with, to the millisecond as notes.
func (s *Store) Now() time.Time { return s.now().UTC().Truncate(time.Millisecond) }

func (s *Store) boardDir(id string) (string, error) {
	if !idPattern.MatchString(id) {
		return "", ErrNotFound
	}
	return filepath.Join(s.dir, id), nil
}

// Create opens a board of b's title, source and origin, with source as its
// text, and returns it.
func (s *Store) Create(b Board, source string) (*Board, error) {
	if err := b.Check(); err != nil {
		return nil, err
	}
	if len(source) > MaxSource {
		return nil, fmt.Errorf("%w: the source is at most %d bytes", ErrTooLarge, MaxSource)
	}
	if !utf8.ValidString(source) {
		return nil, invalid("the source is not UTF-8")
	}
	if strings.TrimSpace(source) == "" {
		return nil, invalid("the source is empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	boards, err := s.list()
	if err != nil {
		return nil, err
	}
	open := 0
	for _, o := range boards {
		if o.Status == Open {
			open++
		}
	}
	if open >= MaxOpen {
		return nil, fmt.Errorf("%w: at most %d boards are open at once; finish or delete one", ErrTooMany, MaxOpen)
	}
	now := s.Now()
	b.Status = Open
	b.CreatedAt, b.UpdatedAt = now, now
	b.Stickies = []Sticky{}
	b.Result = nil
	b.Source.Size = len(source)
	for {
		b.ID = store.NewID()
		dir := filepath.Join(s.dir, b.ID)
		err := os.Mkdir(dir, 0o700)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if err := writeFile(filepath.Join(dir, sourceFile), []byte(source)); err != nil {
			os.RemoveAll(dir)
			return nil, err
		}
		if err := s.write(&b); err != nil {
			os.RemoveAll(dir)
			return nil, err
		}
		return &b, nil
	}
}

const (
	boardFile  = "board.json"
	sourceFile = "source.md"
)

// Get returns board id.
func (s *Store) Get(id string) (*Board, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.read(id)
}

// Source returns the source of board id.
func (s *Store) Source(id string) (string, error) {
	dir, err := s.boardDir(id)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(filepath.Join(dir, sourceFile))
	if errors.Is(err, os.ErrNotExist) {
		return "", ErrNotFound
	}
	return string(b), err
}

// List returns the boards, last updated first.
func (s *Store) List() ([]Board, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.list()
}

func (s *Store) list() ([]Board, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	var boards []Board
	for _, e := range entries {
		if !e.IsDir() || !idPattern.MatchString(e.Name()) {
			continue
		}
		b, err := s.read(e.Name())
		if err != nil {
			continue // a board being made, or broken: not listed
		}
		boards = append(boards, *b)
	}
	slices.SortFunc(boards, func(a, b Board) int {
		if c := b.UpdatedAt.Compare(a.UpdatedAt); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return boards, nil
}

// Update changes board id with change, under the lock, and writes it unless
// change fails. The board is stamped as updated.
func (s *Store) Update(id string, change func(*Board) error) (*Board, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := s.read(id)
	if err != nil {
		return nil, err
	}
	if err := change(b); err != nil {
		return nil, err
	}
	b.UpdatedAt = s.Now()
	if err := s.write(b); err != nil {
		return nil, err
	}
	return b, nil
}

// WorkDir returns the working directory of the Claude Code session ebira
// makes for board id, which it creates if needed: an empty directory of the
// board, deleted with it.
func (s *Store) WorkDir(id string) (string, error) {
	dir, err := s.boardDir(id)
	if err != nil {
		return "", err
	}
	work := filepath.Join(dir, "work")
	return work, os.MkdirAll(work, 0o700)
}

// Delete deletes board id, and returns it as it was: nil when its file
// could not be read, which does not keep it from being deleted.
func (s *Store) Delete(id string) (*Board, error) {
	dir, err := s.boardDir(id)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := s.read(id)
	if errors.Is(err, ErrNotFound) {
		return nil, err
	}
	return b, os.RemoveAll(dir)
}

// Interrupt marks the sticky notes waiting for an answer as failed, as the
// answers were lost with the server that was making them. It is called when
// the server starts.
func (s *Store) Interrupt(reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	boards, err := s.list()
	if err != nil {
		return err
	}
	for _, b := range boards {
		changed := false
		for i := range b.Stickies {
			if st := &b.Stickies[i]; st.State == Waiting {
				st.State, st.Error = Failed, reason
				changed = true
			}
		}
		if changed {
			if err := s.write(&b); err != nil {
				return err
			}
		}
	}
	return nil
}

// Sweep deletes the boards finished more than DoneAge ago, and the open ones
// untouched for more than IdleAge, and returns them.
func (s *Store) Sweep() ([]Board, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	boards, err := s.list()
	if err != nil {
		return nil, err
	}
	now := s.now()
	var gone []Board
	for _, b := range boards {
		old := b.Status == Open && now.Sub(b.UpdatedAt) > IdleAge
		if b.Status == Done {
			at := b.UpdatedAt
			if b.Result != nil {
				at = b.Result.FinishedAt
			}
			old = now.Sub(at) > DoneAge
		}
		if !old {
			continue
		}
		if err := os.RemoveAll(filepath.Join(s.dir, b.ID)); err != nil {
			return gone, err
		}
		gone = append(gone, b)
	}
	return gone, nil
}

func (s *Store) read(id string) (*Board, error) {
	dir, err := s.boardDir(id)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, boardFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var b Board
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("board %s: %w", id, err)
	}
	if b.ID != id {
		return nil, fmt.Errorf("board %s: the file names board %q", id, b.ID)
	}
	if b.Stickies == nil {
		b.Stickies = []Sticky{}
	}
	return &b, nil
}

func (s *Store) write(b *Board) error {
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(filepath.Join(s.dir, b.ID, boardFile), append(data, '\n'))
}

// writeFile writes data to path through a temporary file, so that a reader
// never sees half of it.
func writeFile(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp, 0o600)
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}
