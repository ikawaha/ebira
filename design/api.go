// Package design defines the ebira API with the Goa DSL.
//
//   - api.go: the API and its server
//   - notes.go: the notes service with its types and MCP tools
//   - assistant.go: the assistant service and its agent
package design

import (
	. "goa.design/goa/v3/dsl"
)

var _ = API("ebira", func() {
	Title("ebira API")
	Description("API for Markdown notes stored as YAML files and searched through a SQLite index.")
	Version("0.1.0")
	Server("ebira", func() {
		Services("notes", "assistant")
		Host("localhost", func() { URI("http://localhost:8080") })
	})
})
