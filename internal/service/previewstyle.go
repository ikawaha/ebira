package service

import (
	"context"
	"errors"

	gennotes "github.com/ikawaha/ebira/gen/notes"
	"github.com/ikawaha/ebira/internal/previewstyle"
)

// SetPreviewCSS sets the path of the CSS file for the preview of the web UI,
// whose style preview_style tells and set_preview_style sets. Call it before
// the service is used.
func (s *Notes) SetPreviewCSS(path string) { s.previewCSS = path }

// PreviewStyle tells the style of the preview: the built-in style the CSS file
// for it is or was made from. For people only.
func (s *Notes) PreviewStyle(ctx context.Context, _ *gennotes.PreviewStylePayload) (*gennotes.PreviewStyle, error) {
	if err := peopleOnly(ctx); err != nil {
		return nil, err
	}
	return s.previewStyle()
}

// SetPreviewStyle makes the CSS file for the preview a built-in style, or
// removes it for default, keeping a file that is not a built-in style as it
// is in the backup. For people only.
func (s *Notes) SetPreviewStyle(ctx context.Context, p *gennotes.SetPreviewStylePayload) (*gennotes.PreviewStyle, error) {
	if err := peopleOnly(ctx); err != nil {
		return nil, err
	}
	if s.previewCSS == "" {
		return nil, errors.New("no CSS file for the preview")
	}
	backedUp, err := previewstyle.Use(s.previewCSS, p.Style)
	if err != nil {
		return nil, err
	}
	res, err := s.previewStyle()
	if err != nil {
		return nil, err
	}
	if backedUp {
		b := s.previewCSS + previewstyle.BackupSuffix
		res.Backup = &b
	}
	return res, nil
}

func (s *Notes) previewStyle() (*gennotes.PreviewStyle, error) {
	if s.previewCSS == "" {
		return nil, errors.New("no CSS file for the preview")
	}
	st, err := previewstyle.Read(s.previewCSS)
	if err != nil {
		return nil, err
	}
	return &gennotes.PreviewStyle{Style: st.Style, Modified: st.Modified, Path: s.previewCSS}, nil
}
