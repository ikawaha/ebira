package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	genboards "github.com/ikawaha/ebira/gen/boards"
	gennotes "github.com/ikawaha/ebira/gen/notes"
	"github.com/ikawaha/ebira/internal/board"
	"github.com/ikawaha/ebira/internal/store"
)

// Whiteboards (see internal/board). AI clients open them and read what the
// person gives back; everything else is for people. Questions on sticky notes
// are answered in the background by the assistant, and an event of type board
// tells when an answer came.

// Boards is the boards service: the REST API of whiteboards, for the web UI.
// Its requests are from people as REST tells (APIKeyAuth): every request
// without passkeys (auth: none), and those signed in with a passkey with them.
type Boards struct{ REST }

var _ genboards.Service = Boards{}

// Asker answers a question about a whiteboard with the assistant: the
// question of a sticky note, or a draft of what to give back.
type Asker func(ctx context.Context, question string) (string, error)

// Limits on answering sticky notes.
const (
	// boardAnswers is how many questions are answered at once; the others
	// wait their turn.
	boardAnswers = 2
	// boardAnswerTime bounds one answer, from when its turn comes.
	boardAnswerTime = 3 * time.Minute
	// boardForks is how many forks answer at once: each reads the whole
	// conversation of the session again.
	boardForks = 1
	// boardForkTime bounds one answer of a fork, which may read files.
	boardForkTime = 10 * time.Minute
)

// ForkAsk is a question for a fork of a Claude Code session (Forker).
type ForkAsk struct {
	// Cwd is the working directory of the session.
	Cwd string
	// Resume is the session to resume.
	Resume string
	// Session, when set, forks Resume into a new session of that ID; else
	// the question goes on in Resume.
	Session string
	// Question is the prompt.
	Question string
}

// Forker answers questions in forks of Claude Code sessions, for the sticky
// notes of the fork kind (see internal/claudecode).
type Forker interface {
	// Ask asks a fork the question of a, and returns its answer.
	Ask(ctx context.Context, a ForkAsk) (string, error)
	// Has reports whether Claude Code keeps session id.
	Has(id string) bool
	// Remove deletes session id, one that Ask made.
	Remove(id string) error
}

// eventBoard tells that a whiteboard changed.
const eventBoard = "board"

// BoardPath is where the web UI shows a board, followed by its ID.
const BoardPath = "/#board/"

var errNoBoards = errors.New("whiteboards are not available on this server")

// boards are the whiteboards of the service (see SetBoards).
type boards struct {
	store *board.Store
	ask   Asker // nil when the assistant is not available
	turns chan struct{}
	fork  atomic.Pointer[Forker] // nil until found, and when claude is not found
	forks chan struct{}
	ctx   context.Context // ends with the service, and the answers with it
	stop  context.CancelFunc

	mu      sync.Mutex
	running map[string]context.CancelFunc // answers under way, by board and sticky note
}

// SetBoards gives the service the whiteboards of st, with ask answering the
// questions on them, or nil when the assistant is not available. The sticky
// notes left waiting by an earlier server are marked as failed.
func (s *Notes) SetBoards(st *board.Store, ask Asker) error {
	if err := st.Interrupt("the server stopped before the answer came: ask again"); err != nil {
		return err
	}
	ctx, stop := context.WithCancel(context.Background())
	s.boards = &boards{
		store: st, ask: ask, turns: make(chan struct{}, boardAnswers), forks: make(chan struct{}, boardForks),
		ctx: ctx, stop: stop, running: map[string]context.CancelFunc{},
	}
	return nil
}

// SetForker gives the service the forks of Claude Code sessions, which
// answer the sticky notes of the fork kind; nil when claude is not found.
// Call it after SetBoards, at any time: finding claude may take a while.
func (s *Notes) SetForker(f Forker) {
	if s.boards != nil && f != nil {
		s.boards.fork.Store(&f)
	}
}

// forker returns the forks, or nil.
func (bs *boards) forker() Forker {
	if f := bs.fork.Load(); f != nil {
		return *f
	}
	return nil
}

// runKey is the key of an answer under way in boards.running.
func runKey(board, sticky string) string { return board + "/" + sticky }

// cancel stops the answers under way for the sticky notes of board, or all
// of them without stickies.
func (bs *boards) cancel(id string, stickies []board.Sticky) {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	for key, stop := range bs.running {
		if stickies == nil && strings.HasPrefix(key, id+"/") {
			stop()
		}
	}
	for _, st := range stickies {
		if stop, ok := bs.running[runKey(id, st.ID)]; ok {
			stop()
		}
	}
}

// forget stops the answers of stickies of board, and deletes the Claude Code
// sessions ebira made for them: never the session that opened the board.
func (bs *boards) forget(id string, stickies []board.Sticky) {
	bs.cancel(id, stickies)
	f := bs.forker()
	if f == nil {
		return
	}
	for _, session := range board.Sessions(stickies) {
		if err := f.Remove(session); err != nil {
			log.Printf("whiteboard %s: removing session %s: %v", id, session, err)
		}
	}
}

// SweepBoards deletes the boards finished or left long ago.
func (s *Notes) SweepBoards() error {
	if s.boards == nil {
		return nil
	}
	gone, err := s.boards.store.Sweep()
	for _, b := range gone {
		s.boards.forget(b.ID, b.Stickies)
		s.events.publish(eventBoard, b.ID)
	}
	return err
}

func (s *Notes) boardStore() (*board.Store, error) {
	if s.boards == nil {
		return nil, errNoBoards
	}
	return s.boards.store, nil
}

// boardErr turns an error of internal/board into one of the API.
func boardErr(err error) error {
	switch {
	case errors.Is(err, board.ErrNotFound):
		return gennotes.MakeNotFound(err)
	case errors.Is(err, board.ErrInvalid), errors.Is(err, board.ErrTooMany),
		errors.Is(err, board.ErrTooLarge), errors.Is(err, board.ErrDone):
		return gennotes.MakeBadRequest(err)
	}
	return err
}

func boardURL(ctx context.Context, id string) string {
	base, _ := ctx.Value(baseURLKey{}).(string)
	return base + BoardPath + id
}

// aiBoard returns what an AI client opens a board with.
func aiBoard(title, format string, lang, session, cwd *string) board.Board {
	return board.Board{
		Title:  title,
		Source: board.Source{Format: format, Lang: deref(lang)},
		Origin: board.Origin{AI: true, SessionID: deref(session), Cwd: deref(cwd)},
	}
}

// openBoard opens b with source and tells the clients.
func (s *Notes) openBoard(b board.Board, source string) (*board.Board, error) {
	st, err := s.boardStore()
	if err != nil {
		return nil, err
	}
	nb, err := st.Create(b, source)
	if err != nil {
		return nil, boardErr(err)
	}
	s.events.publish(eventBoard, nb.ID)
	return nb, nil
}

func (s *Notes) OpenAiBoard(ctx context.Context, p *gennotes.OpenAiBoardPayload) (*gennotes.BoardOpened, error) {
	b := aiBoard(p.Title, p.Format, p.Lang, p.SessionID, p.Cwd)
	var source string
	switch {
	case p.Text != nil && p.NoteID != nil:
		return nil, gennotes.MakeBadRequest(errors.New("give text or note_id, not both"))
	case p.Text != nil:
		b.Source.From, source = board.FromText, *p.Text
	case p.NoteID != nil:
		// The request has no mark of people, so a note hidden from AI
		// clients is not found.
		n, err := s.Get(ctx, &gennotes.GetPayload{ID: *p.NoteID})
		if err != nil {
			return nil, err
		}
		b.Source.From, b.Source.NoteID, source = board.FromNote, n.ID, n.Body
	default:
		return nil, gennotes.MakeBadRequest(errors.New("give the text, or the ID of a note"))
	}
	nb, err := s.openBoard(b, source)
	if err != nil {
		return nil, err
	}
	return &gennotes.BoardOpened{ID: nb.ID, URL: boardURL(ctx, nb.ID)}, nil
}

func (s *Notes) StartAiBoard(ctx context.Context, p *gennotes.StartAiBoardPayload) (*gennotes.AIUpload, error) {
	b := aiBoard(p.Title, p.Format, p.Lang, p.SessionID, p.Cwd)
	b.Source.From = board.FromUpload
	// Check what the board is opened with now, rather than when the text comes.
	if _, err := s.boardStore(); err != nil {
		return nil, err
	}
	if err := b.Check(); err != nil {
		return nil, boardErr(err)
	}
	now := s.now()
	u := upload{board: &b, expires: now.Add(uploadMaxAge)}
	token := s.uploads.add(u, now)
	base, _ := ctx.Value(baseURLKey{}).(string)
	return &gennotes.AIUpload{
		URL:       base + BoardUploadsPath + token,
		ExpiresAt: store.FormatTime(u.expires),
		MaxSize:   board.MaxSource,
	}, nil
}

// BoardUploadsPath is the path of the URLs of uploads of boards, followed by
// their tokens.
const BoardUploadsPath = "/boards/uploads/"

func (s *Notes) UploadAiBoard(ctx context.Context, p *gennotes.UploadAiBoardPayload, body io.ReadCloser) (*gennotes.BoardOpened, error) {
	defer body.Close()
	u, ok := s.uploads.take(p.Token, s.now())
	if !ok || u.board == nil {
		return nil, gennotes.MakeNotFound(errors.New("the upload is unknown, used or expired: ask start_ai_board for another URL"))
	}
	b, err := io.ReadAll(io.LimitReader(body, board.MaxSource+1))
	if err != nil {
		return nil, gennotes.MakeBadRequest(err)
	}
	if len(b) > board.MaxSource {
		return nil, gennotes.MakeTooLarge(fmt.Errorf("the text is larger than %d MB", board.MaxSource>>20))
	}
	nb, err := s.openBoard(*u.board, string(b))
	if err != nil {
		return nil, err
	}
	return &gennotes.BoardOpened{ID: nb.ID, URL: boardURL(ctx, nb.ID)}, nil
}

func (s *Notes) GetAiBoard(ctx context.Context, p *gennotes.GetAiBoardPayload) (*gennotes.AIBoard, error) {
	st, err := s.boardStore()
	if err != nil {
		return nil, err
	}
	b, err := st.Get(p.ID)
	if err != nil {
		return nil, boardErr(err)
	}
	res := &gennotes.AIBoard{ID: b.ID, Title: b.Title, Status: b.Status}
	if b.Status == board.Done && b.Result != nil {
		res.Result = &b.Result.Text
		if b.Result.NoteID != "" {
			res.NoteID = &b.Result.NoteID
		}
		res.Stickies = givenBack(b)
	}
	return res, nil
}

// givenBack returns the sticky notes of b given back to the session, in the
// order of the board. Those not chosen stay on the board: neither the user's
// messages nor the answers reach the session.
func givenBack(b *board.Board) []*gennotes.AIBoardSticky {
	chosen := make(map[string]bool, len(b.Result.Stickies))
	for _, id := range b.Result.Stickies {
		chosen[id] = true
	}
	res := []*gennotes.AIBoardSticky{}
	for i := range b.Stickies {
		st := &b.Stickies[i]
		if !chosen[st.ID] {
			continue
		}
		s := &gennotes.AIBoardSticky{ID: st.ID, Parent: optional(st.Parent), Passage: st.Anchor.Exact, Messages: make([]*gennotes.AIBoardMessage, 0, len(st.Messages))}
		for _, m := range st.Messages {
			s.Messages = append(s.Messages, &gennotes.AIBoardMessage{Role: m.Role, Text: m.Text})
		}
		res = append(res, s)
	}
	return res
}

func (r Boards) List(ctx context.Context, _ *genboards.ListPayload) ([]*genboards.BoardSummary, error) {
	s := r.Notes
	if err := peopleOnly(ctx); err != nil {
		return nil, err
	}
	st, err := s.boardStore()
	if err != nil {
		return nil, err
	}
	list, err := st.List()
	if err != nil {
		return nil, err
	}
	res := make([]*genboards.BoardSummary, 0, len(list))
	for _, b := range list {
		waiting := 0
		for _, st := range b.Stickies {
			if st.State == board.Waiting {
				waiting++
			}
		}
		res = append(res, &genboards.BoardSummary{
			ID: b.ID, Title: b.Title, Status: b.Status,
			CreatedAt: store.FormatTime(b.CreatedAt), UpdatedAt: store.FormatTime(b.UpdatedAt),
			Format: b.Source.Format, Stickies: len(b.Stickies), Waiting: waiting,
		})
	}
	return res, nil
}

// getBoard returns board id, for people.
func (s *Notes) getBoard(ctx context.Context, id string) (*board.Store, *board.Board, error) {
	if err := peopleOnly(ctx); err != nil {
		return nil, nil, err
	}
	st, err := s.boardStore()
	if err != nil {
		return nil, nil, err
	}
	b, err := st.Get(id)
	if err != nil {
		return nil, nil, boardErr(err)
	}
	return st, b, nil
}

func (r Boards) Get(ctx context.Context, p *genboards.GetPayload) (*genboards.Board, error) {
	s := r.Notes
	st, b, err := s.getBoard(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	return s.fullBoard(ctx, st, b)
}

// fullBoard returns b with its source, as the API gives it.
func (s *Notes) fullBoard(ctx context.Context, st *board.Store, b *board.Board) (*genboards.Board, error) {
	text, err := st.Source(b.ID)
	if err != nil {
		return nil, boardErr(err)
	}
	res := &genboards.Board{
		ID: b.ID, Title: b.Title, Status: b.Status,
		CreatedAt: store.FormatTime(b.CreatedAt), UpdatedAt: store.FormatTime(b.UpdatedAt),
		Source: &genboards.BoardSource{
			From: b.Source.From, NoteID: optional(b.Source.NoteID), Format: b.Source.Format,
			Lang: optional(b.Source.Lang), Size: b.Source.Size,
		},
		Text:        text,
		Stickies:    make([]*genboards.BoardSticky, 0, len(b.Stickies)),
		FromSession: b.Origin.AI,
		Fork:        s.forkState(ctx, b),
	}
	for i := range b.Stickies {
		res.Stickies = append(res.Stickies, apiSticky(&b.Stickies[i]))
	}
	if r := b.Result; r != nil {
		res.Result = &genboards.BoardResult{Text: r.Text, Stickies: append([]string{}, r.Stickies...), NoteID: optional(r.NoteID), FinishedAt: store.FormatTime(r.FinishedAt)}
	}
	return res, nil
}

// forkState tells whether forks of the session of b can answer its sticky
// notes, for the request ctx: one signed in with a passkey, as a fork runs
// Claude Code on the machine of the server.
func (s *Notes) forkState(ctx context.Context, b *board.Board) *genboards.BoardFork {
	var reason string
	switch {
	case b.Status != board.Open:
		reason = "done"
	case b.Origin.SessionID == "" || b.Origin.Cwd == "":
		reason = "no_session"
	case !signedIn(ctx):
		reason = "auth_none"
	case s.boards == nil || s.boards.forker() == nil:
		reason = "not_found"
	case !s.boards.forker().Has(b.Origin.SessionID):
		reason = "session_missing"
	}
	res := &genboards.BoardFork{Available: reason == "", Cwd: optional(b.Origin.Cwd)}
	if reason != "" {
		res.Reason = &reason
	}
	return res
}

// forkErrors are the errors of a fork that cannot answer, by the reason of
// forkState.
var forkErrors = map[string]string{
	"done":            "the board is finished",
	"no_session":      "no Claude Code session opened the board",
	"auth_none":       "forks need passkeys: sign in with a passkey (ebira auth passkey)",
	"not_found":       "claude (Claude Code) is not found on the machine of the server",
	"session_missing": "Claude Code no longer has the session that opened the board",
}

// requireFork fails unless forks of the session of b can answer.
func (s *Notes) requireFork(ctx context.Context, b *board.Board) error {
	if st := s.forkState(ctx, b); !st.Available {
		return gennotes.MakeUnavailable(errors.New(forkErrors[*st.Reason]))
	}
	return nil
}

func apiSticky(s *board.Sticky) *genboards.BoardSticky {
	res := &genboards.BoardSticky{
		ID: s.ID, Parent: optional(s.Parent),
		Anchor: &genboards.BoardAnchor{Exact: s.Anchor.Exact, Prefix: optional(s.Anchor.Prefix), Suffix: optional(s.Anchor.Suffix)},
		Kind:   s.Kind, Messages: make([]*genboards.BoardMessage, 0, len(s.Messages)),
		State: s.State, Error: optional(s.Error), Session: optional(s.Session),
		CreatedAt: store.FormatTime(s.CreatedAt), UpdatedAt: store.FormatTime(s.UpdatedAt),
	}
	for _, m := range s.Messages {
		res.Messages = append(res.Messages, &genboards.BoardMessage{Role: m.Role, Text: m.Text, At: store.FormatTime(m.At)})
	}
	return res
}

// optional returns a pointer to s, or nil for the empty string.
func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (r Boards) Open(ctx context.Context, p *genboards.OpenPayload) (*genboards.Board, error) {
	s := r.Notes
	if err := peopleOnly(ctx); err != nil {
		return nil, err
	}
	n, err := s.Get(ctx, &gennotes.GetPayload{ID: p.NoteID})
	if err != nil {
		return nil, err
	}
	title := strings.TrimSpace(deref(p.Title))
	if title == "" {
		title = n.Title
	}
	nb, err := s.openBoard(board.Board{
		Title:  title,
		Source: board.Source{From: board.FromNote, NoteID: n.ID, Format: board.Markdown},
	}, n.Body)
	if err != nil {
		return nil, err
	}
	return s.fullBoard(ctx, s.boards.store, nb)
}

// changeBoard changes board id with change, for people, and tells the
// clients.
func (s *Notes) changeBoard(ctx context.Context, id string, change func(*board.Board) error) (*board.Store, *board.Board, error) {
	if err := peopleOnly(ctx); err != nil {
		return nil, nil, err
	}
	st, err := s.boardStore()
	if err != nil {
		return nil, nil, err
	}
	b, err := st.Update(id, change)
	if err != nil {
		return nil, nil, boardErr(err)
	}
	s.events.publish(eventBoard, id)
	return st, b, nil
}

func (r Boards) Save(ctx context.Context, p *genboards.SavePayload) (*genboards.Board, error) {
	s := r.Notes
	st, b, err := s.changeBoard(ctx, p.ID, func(b *board.Board) error {
		title := strings.TrimSpace(p.Title)
		if title == "" {
			return fmt.Errorf("%w: the title is empty", board.ErrInvalid)
		}
		b.Title = title
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.fullBoard(ctx, st, b)
}

func (r Boards) Delete(ctx context.Context, p *genboards.DeletePayload) error {
	s := r.Notes
	if err := peopleOnly(ctx); err != nil {
		return err
	}
	st, err := s.boardStore()
	if err != nil {
		return err
	}
	b, err := st.Delete(p.ID)
	if err != nil {
		return boardErr(err)
	}
	if b != nil {
		s.boards.forget(b.ID, b.Stickies)
	} else {
		s.boards.cancel(p.ID, nil)
	}
	s.events.publish(eventBoard, p.ID)
	return nil
}

// stickyOf returns the sticky note id of b, or not_found.
func stickyOf(b *board.Board, id string) (*board.Sticky, error) {
	st := b.Find(id)
	if st == nil {
		return nil, fmt.Errorf("%w: no sticky note %q", board.ErrNotFound, id)
	}
	return st, nil
}

// requireOpen fails for a finished board.
func requireOpen(b *board.Board) error {
	if b.Status != board.Open {
		return board.ErrDone
	}
	return nil
}

func (r Boards) AddSticky(ctx context.Context, p *genboards.AddStickyPayload) (*genboards.BoardSticky, error) {
	s := r.Notes
	if err := peopleOnly(ctx); err != nil {
		return nil, err
	}
	text := deref(p.Text)
	asks := p.Kind == board.Assistant || p.Kind == board.Fork
	if p.Kind == board.Assistant {
		if err := s.requireAsker(); err != nil {
			return nil, err
		}
	}
	if asks && strings.TrimSpace(text) == "" {
		return nil, gennotes.MakeBadRequest(errors.New("ask a question"))
	}
	if p.Parent != nil || p.Kind == board.Fork {
		_, b, err := s.getBoard(ctx, p.ID)
		if err != nil {
			return nil, err
		}
		// A sticky note on a missing parent is not found, rather than invalid.
		if p.Parent != nil && b.Find(*p.Parent) == nil {
			return nil, gennotes.MakeNotFound(fmt.Errorf("no sticky note %q", *p.Parent))
		}
		if p.Kind == board.Fork {
			if err := s.requireFork(ctx, b); err != nil {
				return nil, err
			}
		}
	}
	var added board.Sticky
	_, _, err := s.changeBoard(ctx, p.ID, func(b *board.Board) error {
		in := board.Sticky{
			Parent: deref(p.Parent),
			Kind:   p.Kind,
			Anchor: board.Anchor{Exact: p.Anchor.Exact, Prefix: deref(p.Anchor.Prefix), Suffix: deref(p.Anchor.Suffix)},
		}
		if strings.TrimSpace(text) != "" {
			in.Messages = []board.Message{{Role: board.User, Text: text}}
		}
		if p.Kind == board.Fork {
			in.Session = uuid.NewString()
		}
		st, err := b.Add(in, s.now())
		if err != nil {
			return err
		}
		if asks {
			st.State = board.Waiting
		}
		added = *st
		return nil
	})
	if err != nil {
		return nil, err
	}
	if asks {
		s.answer(p.ID, added.ID, added.Kind)
	}
	return apiSticky(&added), nil
}

func (r Boards) SaveSticky(ctx context.Context, p *genboards.SaveStickyPayload) (*genboards.BoardSticky, error) {
	s := r.Notes
	var saved board.Sticky
	_, _, err := s.changeBoard(ctx, p.ID, func(b *board.Board) error {
		if err := requireOpen(b); err != nil {
			return err
		}
		st, err := stickyOf(b, p.Sticky)
		if err != nil {
			return err
		}
		if p.Text != nil {
			if st.Kind != board.Memo {
				return fmt.Errorf("%w: only the text of a memo changes", board.ErrInvalid)
			}
			if t := *p.Text; strings.TrimSpace(t) == "" {
				st.Messages = []board.Message{}
			} else if len(t) > board.MaxMessage {
				return fmt.Errorf("%w: a message is at most %d bytes", board.ErrTooLarge, board.MaxMessage)
			} else if len(st.Messages) == 0 {
				st.Messages = []board.Message{{Role: board.User, Text: t, At: s.now()}}
			} else {
				st.Messages = []board.Message{{Role: board.User, Text: t, At: st.Messages[0].At}}
			}
		}
		st.UpdatedAt = s.now()
		saved = *st
		return nil
	})
	if err != nil {
		return nil, err
	}
	return apiSticky(&saved), nil
}

func (r Boards) DeleteSticky(ctx context.Context, p *genboards.DeleteStickyPayload) error {
	s := r.Notes
	var removed []board.Sticky
	_, _, err := s.changeBoard(ctx, p.ID, func(b *board.Board) error {
		if err := requireOpen(b); err != nil {
			return err
		}
		if removed = b.Remove(p.Sticky); removed == nil {
			return fmt.Errorf("%w: no sticky note %q", board.ErrNotFound, p.Sticky)
		}
		return nil
	})
	if err != nil {
		return err
	}
	// Deleting a sticky note waiting for an answer stops it.
	s.boards.forget(p.ID, removed)
	return nil
}

func (r Boards) AskSticky(ctx context.Context, p *genboards.AskStickyPayload) (*genboards.BoardSticky, error) {
	s := r.Notes
	text := deref(p.Text)
	var asked board.Sticky
	asks := false
	_, _, err := s.changeBoard(ctx, p.ID, func(b *board.Board) error {
		if err := requireOpen(b); err != nil {
			return err
		}
		st, err := stickyOf(b, p.Sticky)
		if err != nil {
			return err
		}
		if st.State == board.Waiting {
			return fmt.Errorf("%w: the sticky note waits for an answer", board.ErrInvalid)
		}
		if st.Kind == board.Assistant || st.Kind == board.Fork {
			if st.Kind == board.Assistant {
				if err := s.requireAsker(); err != nil {
					return err
				}
			} else if err := s.requireFork(ctx, b); err != nil {
				return err
			}
			asks = true
			n := len(st.Messages)
			retry := strings.TrimSpace(text) == "" && st.State == board.Failed && n > 0 && st.Messages[n-1].Role == board.User
			if !retry {
				if err := st.Say(board.User, text, s.now()); err != nil {
					return err
				}
			}
			st.State, st.Error = board.Waiting, ""
		} else if err := st.Say(board.User, text, s.now()); err != nil {
			return err
		}
		asked = *st
		return nil
	})
	if err != nil {
		return nil, err
	}
	if asks {
		s.answer(p.ID, asked.ID, asked.Kind)
	}
	return apiSticky(&asked), nil
}

func (s *Notes) requireAsker() error {
	if s.boards == nil || s.boards.ask == nil {
		return gennotes.MakeUnavailable(errors.New("the assistant is not configured: set a model API key and restart the server"))
	}
	return nil
}

// answer asks the assistant, or a fork for kind fork, the question of sticky
// note sid of board id, in the background, and writes the answer, or why it
// failed, on the sticky note. Deleting the sticky note or the board stops it.
func (s *Notes) answer(id, sid, kind string) {
	bs := s.boards
	turns, limit := bs.turns, boardAnswerTime
	if kind == board.Fork {
		turns, limit = bs.forks, boardForkTime
	}
	ctx, cancel := context.WithCancel(bs.ctx)
	key := runKey(id, sid)
	bs.mu.Lock()
	bs.running[key] = cancel
	bs.mu.Unlock()
	go func() {
		defer func() {
			bs.mu.Lock()
			delete(bs.running, key)
			bs.mu.Unlock()
			cancel()
		}()
		select {
		case turns <- struct{}{}:
			defer func() { <-turns }()
		case <-ctx.Done():
			return
		}
		b, err := bs.store.Get(id)
		if err != nil {
			return // deleted meanwhile
		}
		st := b.Find(sid)
		if st == nil || st.State != board.Waiting {
			return
		}
		source, err := bs.store.Source(id)
		if err != nil {
			return
		}
		actx, acancel := context.WithTimeout(ctx, limit)
		var reply string
		if kind == board.Fork {
			reply, err = bs.askFork(actx, b, source, st)
		} else {
			reply, err = bs.ask(actx, board.Question(b, source, st))
		}
		timedOut := errors.Is(actx.Err(), context.DeadlineExceeded)
		acancel()
		if bs.ctx.Err() != nil {
			return // the server stops; SetBoards marks it next time
		}
		if ctx.Err() != nil {
			// The sticky note or the board was deleted: a fork may have made
			// its session meanwhile.
			if kind == board.Fork {
				bs.forget(id, []board.Sticky{*st})
			}
			return
		}
		if timedOut {
			err = fmt.Errorf("no answer in %v", limit)
		}
		_, uerr := bs.store.Update(id, func(b *board.Board) error {
			st := b.Find(sid)
			if st == nil || st.State != board.Waiting {
				return errGone
			}
			if err == nil {
				err = st.Say(board.Agent, reply, bs.store.Now())
			}
			if err != nil {
				log.Printf("whiteboard %s: answer of %s: %v", id, sid, err)
				st.State, st.Error = board.Failed, err.Error()
			} else {
				st.State, st.Error = board.Idle, ""
			}
			st.UpdatedAt = bs.store.Now()
			return nil
		})
		if uerr == nil {
			s.events.publish(eventBoard, id)
		}
	}()
}

var errGone = errors.New("the sticky note is gone")

// askFork asks a fork the question of sticky note st of b, whose source is
// source. The first question forks the session of the parent sticky note, if
// a fork answers it, else that of the board, into the session of st; the next
// ones go on in it.
func (bs *boards) askFork(ctx context.Context, b *board.Board, source string, st *board.Sticky) (string, error) {
	f := bs.forker()
	if f == nil {
		return "", errors.New(forkErrors["not_found"])
	}
	resumed := f.Has(st.Session)
	a := ForkAsk{Cwd: b.Origin.Cwd, Question: board.ForkQuestion(b, source, st, resumed)}
	if resumed {
		a.Resume = st.Session
	} else {
		a.Resume, a.Session = b.Origin.SessionID, st.Session
		if p := b.Find(st.Parent); p != nil && p.Kind == board.Fork && f.Has(p.Session) {
			a.Resume = p.Session
		}
	}
	return f.Ask(ctx, a)
}

func (r Boards) Draft(ctx context.Context, p *genboards.DraftPayload) (*genboards.DraftResult, error) {
	s := r.Notes
	_, b, err := s.getBoard(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	if err := requireOpen(b); err != nil {
		return nil, boardErr(err)
	}
	if len(p.Stickies) == 0 {
		return nil, gennotes.MakeBadRequest(errors.New("choose the sticky notes to give back first"))
	}
	for _, id := range p.Stickies {
		if b.Find(id) == nil {
			return nil, gennotes.MakeBadRequest(fmt.Errorf("no sticky note %q", id))
		}
	}
	if err := s.requireAsker(); err != nil {
		return nil, err
	}
	actx, cancel := context.WithTimeout(ctx, boardAnswerTime)
	defer cancel()
	text, err := s.boards.ask(actx, board.DraftQuestion(b, p.Stickies))
	if err != nil {
		log.Printf("whiteboard %s: draft: %v", p.ID, err)
		return nil, gennotes.MakeUnavailable(fmt.Errorf("the assistant did not draft: %w", err))
	}
	return &genboards.DraftResult{Text: text}, nil
}

func (r Boards) Finish(ctx context.Context, p *genboards.FinishPayload) (*genboards.Board, error) {
	s := r.Notes
	st, b, err := s.getBoard(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	ready := func(b *board.Board) error {
		if err := requireOpen(b); err != nil {
			return err
		}
		for _, st := range b.Stickies {
			if st.State == board.Waiting {
				return fmt.Errorf("%w: a sticky note waits for an answer", board.ErrInvalid)
			}
		}
		return nil
	}
	if err := ready(b); err != nil {
		return nil, boardErr(err)
	}
	// The sticky notes to give back, once each, and all on the board.
	var given []string
	seen := map[string]bool{}
	for _, id := range p.Stickies {
		if b.Find(id) == nil {
			return nil, gennotes.MakeBadRequest(fmt.Errorf("no sticky note %q", id))
		}
		if !seen[id] {
			seen[id] = true
			given = append(given, id)
		}
	}
	noteID := ""
	if p.Notebook != nil {
		source, err := st.Source(b.ID)
		if err != nil {
			return nil, boardErr(err)
		}
		body := board.NoteBody(b, source, p.Text, p.Lang)
		n, err := s.Create(ctx, &gennotes.CreatePayload{
			Notebook: p.Notebook, Title: b.Title, Tags: []string{board.Tag}, Body: &body,
		})
		if err != nil {
			return nil, err
		}
		noteID = n.ID
	}
	st, b, err = s.changeBoard(ctx, p.ID, func(b *board.Board) error {
		if err := ready(b); err != nil {
			return err
		}
		b.Status = board.Done
		b.Result = &board.Result{Text: p.Text, Stickies: given, NoteID: noteID, FinishedAt: s.now()}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.fullBoard(ctx, st, b)
}
