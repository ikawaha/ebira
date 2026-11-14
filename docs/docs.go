// Package docs holds the documentation of ebira for each language (en, ja),
// which is also the guide the server adds to the notes (see internal/guide):
// Markdown files, and guide.yaml, which tells their order in the guide.
package docs

import "embed"

// Guide is the documentation by language: <lang>/guide.yaml and the notes it
// names.
//
//go:embed en ja
var Guide embed.FS
