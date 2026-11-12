package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	goahttp "goa.design/goa/v3/http"

	notessvr "github.com/ikawaha/ebira/gen/http/notes/server"
	gennotes "github.com/ikawaha/ebira/gen/notes"
)

func TestUploadAiFile(t *testing.T) {
	ai := WithBaseURL(context.Background(), "http://localhost:13579")
	person := AsPerson(context.Background())
	svc, _ := newTestService(t)
	shared, err := svc.CreateAiNotebook(ai, &gennotes.CreateAiNotebookPayload{Name: "shared"})
	if err != nil {
		t.Fatal(err)
	}
	n, err := svc.CreateAiNote(ai, &gennotes.CreateAiNotePayload{Notebook: shared.ID, Title: "chart"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(person, &gennotes.CreatePayload{ID: ptr("mine"), Notebook: ptr("work"), Title: "mine"}); err != nil {
		t.Fatal(err)
	}
	start := func(id string) (string, error) {
		u, err := svc.StartAiUpload(ai, &gennotes.StartAiUploadPayload{ID: id, Name: "Chart 1.png"})
		if err != nil {
			return "", err
		}
		token, ok := strings.CutPrefix(u.URL, "http://localhost:13579"+UploadsPath)
		if !ok || token == "" || u.MaxSize != aiMaxFileSize {
			t.Fatalf("StartAiUpload = %+v", u)
		}
		return token, nil
	}
	send := func(token string, content []byte) (*gennotes.NoteFile, error) {
		return svc.UploadAiFile(context.Background(), &gennotes.UploadAiFilePayload{Token: token}, io.NopCloser(bytes.NewReader(content)))
	}

	token, err := start(n.ID)
	if err != nil {
		t.Fatal(err)
	}
	f, err := send(token, pngHead)
	if err != nil {
		t.Fatal(err)
	}
	if f.Name != "Chart-1.png" || f.Path != n.ID+".assets/Chart-1.png" {
		t.Errorf("UploadAiFile = %+v", f)
	}
	// A URL works once.
	if _, err := send(token, pngHead); errorName(err) != "not_found" {
		t.Errorf("a used URL: %v", err)
	}
	if _, err := send("unknown", pngHead); errorName(err) != "not_found" {
		t.Errorf("an unknown URL: %v", err)
	}
	// The notes AI clients may not write get no URL.
	for _, id := range []string{"mine", "missing"} {
		if _, err := start(id); err == nil {
			t.Errorf("StartAiUpload %s: no error", id)
		}
	}
	// Too large, and the URL is used.
	token, _ = start(n.ID)
	if _, err := send(token, append(bytes.Clone(pngHead), make([]byte, aiMaxFileSize)...)); errorName(err) != "too_large" {
		t.Errorf("too large: %v", err)
	}
	// Expired.
	token, _ = start(n.ID)
	now := svc.now
	svc.now = func() time.Time { return now().Add(uploadMaxAge) }
	if _, err := send(token, pngHead); errorName(err) != "not_found" {
		t.Errorf("an expired URL: %v", err)
	}
	svc.now = now
	// The notebook became private after the URL was given.
	token, _ = start(n.ID)
	if err := svc.MakeNotebookPrivate(ai, &gennotes.MakeNotebookPrivatePayload{ID: shared.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := send(token, pngHead); errorName(err) != "not_found" {
		t.Errorf("a note made private: %v", err)
	}
}

// The oldest pending uploads go when there are too many.
func TestUploadsBound(t *testing.T) {
	var us uploads
	now := time.Now()
	first := us.add(upload{id: "a", expires: now.Add(time.Minute)}, now)
	for i := range maxUploads {
		us.add(upload{id: "b", expires: now.Add(time.Minute + time.Duration(i+1)*time.Second)}, now)
	}
	if len(us.pending) != maxUploads {
		t.Errorf("%d pending, want %d", len(us.pending), maxUploads)
	}
	if _, ok := us.take(first, now); ok {
		t.Error("the oldest upload is kept")
	}
}

// The upload goes through the REST API without a session, even when passkeys
// are required, and is from AI: the token allows it.
func TestUploadOverHTTP(t *testing.T) {
	svc, _ := newTestService(t)
	ai := context.Background()
	shared, err := svc.CreateAiNotebook(ai, &gennotes.CreateAiNotebookPayload{Name: "shared"})
	if err != nil {
		t.Fatal(err)
	}
	n, err := svc.CreateAiNote(ai, &gennotes.CreateAiNotePayload{Notebook: shared.ID, Title: "chart"})
	if err != nil {
		t.Fatal(err)
	}
	mux := goahttp.NewMuxer()
	rest := REST{Notes: svc, Session: func(string) (bool, bool) { return false, false }}
	notessvr.Mount(mux, notessvr.New(gennotes.NewEndpoints(rest), mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	u, err := svc.StartAiUpload(WithBaseURL(ai, srv.URL), &gennotes.StartAiUploadPayload{ID: n.ID, Name: "report.pdf"})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPut, u.URL, strings.NewReader("%PDF-1.7\n"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var f gennotes.NoteFile
	if err := json.NewDecoder(res.Body).Decode(&f); err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusCreated || f.Path != n.ID+".assets/report.pdf" {
		t.Errorf("PUT %s: %d %+v", u.URL, res.StatusCode, f)
	}
}
