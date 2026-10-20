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

// revDSL is the pattern and an example of the ID of a version of a note.
var revDSL = func() {
	Pattern(`^[0-9]{1,20}$`)
	Example("1792400000000000000")
}

// timeDSL is the format and an example of a time.
var timeDSL = func() {
	Format(FormatDateTime)
	Example("2026-10-16T09:30:00Z")
}

// sessionCookie is the name of the cookie of the session of people signed in
// with a passkey.
const sessionCookie = "ebira_session"

// SessionAuth is the session of people signed in with a passkey.
var SessionAuth = APIKeySecurity("session", func() {
	Description("Session of people signed in with a passkey, sent as the " + sessionCookie + " cookie. While the server requires passkeys (auth: passkey), only a REST request with a valid session passes, as from people, and the others fail with unauthorized (AI clients use MCP, which does not take sessions); without passkeys (auth: none), every REST request is from people")
})

// sessionDSL is the payload attribute of SessionAuth.
var sessionDSL = func() {
	APIKey("session", "session", String, "Session of people signed in with a passkey, set as a cookie by the server")
}

// sessionCookieDSL sets the session cookie of a response: only for the
// server (HttpOnly), not sent from other sites (SameSite=Strict).
var sessionCookieDSL = func() {
	Cookie("session:" + sessionCookie)
	CookiePath("/")
	CookieHTTPOnly()
	CookieSameSite(CookieSameSiteStrict)
	CookieMaxAge(sessionMaxAge)
}

// sessionMaxAge is the lifetime of a session in seconds: 30 days.
const sessionMaxAge = 30 * 24 * 60 * 60
