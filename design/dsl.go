package design

import . "goa.design/goa/v3/dsl"

// RequiredAttribute defines an attribute and marks it as required, so that a
// definition reads attribute by attribute instead of with a separate Required
// list.
func RequiredAttribute(name string, args ...any) {
	Attribute(name, args...)
	Required(name)
}

// idPattern is the pattern of note and notebook IDs.
const idPattern = `^[a-z0-9][a-z0-9_-]{0,63}$`

// noteIDDSL is the pattern and an example of a note ID.
var noteIDDSL = func() {
	Pattern(idPattern)
	Example("be4c1193735ee1")
}

// notebookIDDSL is the pattern and an example of a notebook ID.
var notebookIDDSL = func() {
	Pattern(idPattern)
	Example("inbox")
}

// timeDSL is the format and an example of a time.
var timeDSL = func() {
	Format(FormatDateTime)
	Example("2026-10-16T09:30:00Z")
}
