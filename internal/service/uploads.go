package service

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"sync"
	"time"

	gennotes "github.com/ikawaha/ebira/gen/notes"
	"github.com/ikawaha/ebira/internal/store"
)

// An AI client with a shell sends a file as is rather than in base64, which
// it would have to write out as tokens: StartAiUpload gives a URL for one
// file, and UploadAiFile takes the file sent to it. The URL is the right to
// add that one file to that one note, once and for a short time, so the
// endpoint has no other check; the note is checked again when the file comes,
// as it may have moved meanwhile. Uploads are kept in memory only.

// Lifetimes and bounds of uploads.
const (
	uploadMaxAge = 10 * time.Minute
	// maxUploads bounds the pending uploads; the oldest goes first.
	maxUploads = 32
)

var errUnknownUpload = errors.New("the upload is unknown, used or expired: ask start_ai_upload for another URL")

// UploadsPath is the path of the URLs of uploads, followed by their tokens.
const UploadsPath = "/uploads/"

// upload is a pending upload.
type upload struct {
	id, name string // the note and the name of the file
	expires  time.Time
}

// uploads are the pending uploads by their tokens.
type uploads struct {
	mu      sync.Mutex
	pending map[string]upload
}

// add makes a token for u, forgetting expired uploads, and the oldest when
// there are too many.
func (us *uploads) add(u upload, now time.Time) string {
	us.mu.Lock()
	defer us.mu.Unlock()
	if us.pending == nil {
		us.pending = map[string]upload{}
	}
	for t, p := range us.pending {
		if !now.Before(p.expires) {
			delete(us.pending, t)
		}
	}
	for len(us.pending) >= maxUploads {
		oldest := ""
		for t, p := range us.pending {
			if oldest == "" || p.expires.Before(us.pending[oldest].expires) {
				oldest = t
			}
		}
		delete(us.pending, oldest)
	}
	token := rand.Text()
	us.pending[token] = u
	return token
}

// take returns the upload of token and forgets it, so that it works once.
func (us *uploads) take(token string, now time.Time) (upload, bool) {
	us.mu.Lock()
	defer us.mu.Unlock()
	u, ok := us.pending[token]
	delete(us.pending, token)
	return u, ok && now.Before(u.expires)
}

type baseURLKey struct{}

// WithBaseURL tells the service the URL a request came to, such as
// http://localhost:13579, for the URLs it gives back (StartAiUpload).
func WithBaseURL(ctx context.Context, base string) context.Context {
	return context.WithValue(ctx, baseURLKey{}, base)
}

// StartAiUpload gives an AI client a URL to send a file of a note in a
// notebook of the collab kind to. A note hidden from AI clients is not found.
func (s *Notes) StartAiUpload(ctx context.Context, p *gennotes.StartAiUploadPayload) (*gennotes.AIUpload, error) {
	n, err := s.Get(ctx, &gennotes.GetPayload{ID: p.ID})
	if err != nil {
		return nil, err
	}
	if err := s.requireAINotebook(n.Notebook); err != nil {
		return nil, err
	}
	base, _ := ctx.Value(baseURLKey{}).(string)
	now := s.now()
	u := upload{id: p.ID, name: p.Name, expires: now.Add(uploadMaxAge)}
	token := s.uploads.add(u, now)
	return &gennotes.AIUpload{
		URL:       base + UploadsPath + token,
		ExpiresAt: store.FormatTime(u.expires),
		MaxSize:   aiMaxFileSize,
	}, nil
}

// UploadAiFile adds the file sent to a URL from StartAiUpload, as AddAiFile
// does.
func (s *Notes) UploadAiFile(ctx context.Context, p *gennotes.UploadAiFilePayload, body io.ReadCloser) (*gennotes.NoteFile, error) {
	defer body.Close()
	u, ok := s.uploads.take(p.Token, s.now())
	if !ok {
		return nil, gennotes.MakeNotFound(errUnknownUpload)
	}
	// One byte more than allowed tells a file that is too large.
	b, err := io.ReadAll(io.LimitReader(body, aiMaxFileSize+1))
	if err != nil {
		return nil, gennotes.MakeBadRequest(err)
	}
	// The request has no mark of people, so this checks the note as AddAiFile
	// does.
	return s.AddAiFile(ctx, &gennotes.AddAiFilePayload{ID: u.id, Name: u.name, Content: b})
}
