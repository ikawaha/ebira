package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	gennotes "github.com/ikawaha/ebira/gen/notes"
)

func TestPreviewStyle(t *testing.T) {
	person := AsPerson(context.Background())
	svc, _ := newTestService(t)
	path := filepath.Join(t.TempDir(), "preview.css")
	svc.SetPreviewCSS(path)

	st, err := svc.PreviewStyle(person, nil)
	// Without the file, the default style, band.
	if err != nil || st.Style != "band" || st.Path != path {
		t.Fatalf("PreviewStyle = %+v, %v", st, err)
	}
	st, err = svc.SetPreviewStyle(person, &gennotes.SetPreviewStylePayload{Style: "document"})
	if err != nil || st.Style != "document" || st.Modified || st.Backup != nil {
		t.Fatalf("SetPreviewStyle(document) = %+v, %v", st, err)
	}
	// Changed by the user, it is kept when replaced.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("h1 { color: red }\n")
	f.Close()
	if st, _ := svc.PreviewStyle(person, nil); st.Style != "document" || !st.Modified {
		t.Errorf("changed: %+v", st)
	}
	// The default style removes the file; default, as an earlier web UI
	// named it, does too.
	st, err = svc.SetPreviewStyle(person, &gennotes.SetPreviewStylePayload{Style: "band"})
	if err != nil || st.Style != "band" || st.Backup == nil || *st.Backup != path+".bak" {
		t.Fatalf("SetPreviewStyle(band) = %+v, %v", st, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the file is left: %v", err)
	}
	if _, err := svc.SetPreviewStyle(person, &gennotes.SetPreviewStylePayload{Style: "plain"}); err != nil {
		t.Fatal(err)
	}
	st, err = svc.SetPreviewStyle(person, &gennotes.SetPreviewStylePayload{Style: "default"})
	if err != nil || st.Style != "band" || st.Backup != nil {
		t.Fatalf("SetPreviewStyle(default) = %+v, %v", st, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the file is left: %v", err)
	}
}
