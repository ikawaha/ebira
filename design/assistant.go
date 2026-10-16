package design

import (
	. "goa.design/goa-ai/dsl"
	. "goa.design/goa/v3/dsl"
)

var _ = Service("assistant", func() {
	Description("Answers questions about the notes with the assistant agent.")

	// assistant answers questions about the user's notes, reading them through
	// the search and get methods of the notes service.
	Agent("assistant", "Answer questions using the user's notes", func() {
		Use("notes", func() {
			Tool("search", "Full-text search over notes. Japanese terms are supported", func() {
				BindTo("notes", "search")
			})
			Tool("get", "Get the full content of a note by ID", func() {
				// Only the ID; the highlight query of the method is for UIs.
				Args(func() {
					RequiredAttribute("id", String, "Note ID", noteIDDSL)
				})
				BindTo("notes", "get")
			})
		})
		RunPolicy(func() {
			DefaultCaps(MaxToolCalls(6), MaxRecoveryTurns(2))
			TimeBudget("60s")
		})
	})

	Error("unavailable", ErrorResult, "The assistant is not configured, such as when no model API key is set")
	HTTP(func() {
		Response("unavailable", StatusServiceUnavailable)
	})

	Method("ask", func() {
		Description("Ask a question about the notes. The agent searches and reads the notes, then answers citing the notes it used. Each question is answered on its own, without the earlier ones.")
		Payload(func() {
			RequiredAttribute("question", String, "Question", func() {
				MinLength(1)
				MaxLength(2000)
			})
		})
		Result(func() {
			RequiredAttribute("answer", String, "Answer in Markdown")
		})
		HTTP(func() {
			POST("/ask")
		})
	})
})
