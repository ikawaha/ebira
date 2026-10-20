package design

import (
	. "goa.design/goa/v3/dsl"
)

// Ceremony is the first step of registering or signing in with a passkey: the
// options for the browser and the ID of the ceremony to finish.
var Ceremony = Type("Ceremony", func() {
	RequiredAttribute("ceremony", String, "ID of the ceremony, passed back when finishing it")
	RequiredAttribute("options", Any, "Options of the ceremony for the browser, in the JSON form of the Web Authentication API: PublicKeyCredentialCreationOptionsJSON for a registration, PublicKeyCredentialRequestOptionsJSON for a sign-in")
})

// PasskeyInfo is a registered passkey.
var PasskeyInfo = Type("PasskeyInfo", func() {
	RequiredAttribute("id", String, "ID of the passkey")
	RequiredAttribute("created_at", String, "When the passkey was registered; empty for those registered before ebira kept it", func() {
		Example("2026-10-20T09:30:00Z")
	})
	Attribute("used_at", String, "When the passkey was last used to sign in", timeDSL)
})

// CeremonyFinish is the answer of the browser to a ceremony.
var CeremonyFinish = Type("CeremonyFinish", func() {
	RequiredAttribute("ceremony", String, "ID of the ceremony, from its first step")
	RequiredAttribute("credential", Any, "Credential the browser created or used, as PublicKeyCredential.toJSON returns it")
})

var _ = Service("auth", func() {
	Description("Passkeys: registering them and signing in with them, in the web UI and, through the web UI, in clients without a browser such as the TUI (a device flow). Not for AI clients, and not on MCP.")

	Method("status", func() {
		Description("Tell how the server authenticates people and whether the request is signed in.")
		Payload(func() {
			Attribute("session", String, "Session cookie")
		})
		Result(func() {
			RequiredAttribute("mode", String, "How the server authenticates people: none treats every REST request as from people; passkey requires signing in with a passkey", func() {
				Enum("none", "passkey")
			})
			RequiredAttribute("registered", Boolean, "Whether a passkey is registered")
			RequiredAttribute("signed_in", Boolean, "Whether the request has a valid session")
		})
		HTTP(func() {
			GET("/auth")
			Cookie("session:" + sessionCookie)
		})
	})

	Method("register_begin", func() {
		Description("Begin registering a passkey: with the one-time token of the URL that ebira auth passkey prints in the terminal, so that only people at the terminal can register the first passkey, or, to add another passkey, without a token from the web UI signed in and verified with a passkey.")
		Payload(func() {
			Attribute("token", String, "One-time registration token; omit it to add a passkey from a verified session")
			Attribute("session", String, "Session cookie")
		})
		Result(Ceremony)
		Error("forbidden", ErrorResult, "The token is unknown, used or expired")
		Error("verification_required", ErrorResult, "Without a token: the session is missing, or not signed in with a passkey recently")
		HTTP(func() {
			POST("/auth/passkey/register/begin")
			Cookie("session:" + sessionCookie)
			Response("forbidden", StatusForbidden)
			Response("verification_required", StatusForbidden)
		})
	})

	Method("register_finish", func() {
		Description("Finish registering a passkey with the credential the browser created.")
		Payload(CeremonyFinish)
		Error("bad_request", ErrorResult, "The ceremony is unknown or expired, or the credential is invalid")
		HTTP(func() {
			POST("/auth/passkey/register/finish")
			Response(StatusNoContent)
			Response("bad_request", StatusBadRequest)
		})
	})

	Method("login_begin", func() {
		Description("Begin signing in with a passkey.")
		Result(Ceremony)
		Error("bad_request", ErrorResult, "No passkey is registered")
		HTTP(func() {
			POST("/auth/passkey/login/begin")
			Response("bad_request", StatusBadRequest)
		})
	})

	Method("login_finish", func() {
		Description("Finish signing in with the passkey the browser used. The response sets the session cookie.")
		Payload(CeremonyFinish)
		Result(func() {
			RequiredAttribute("session", String, "Session")
		})
		Error("unauthorized", ErrorResult, "The ceremony is unknown or expired, or the passkey is not registered or its signature is invalid")
		HTTP(func() {
			POST("/auth/passkey/login/finish")
			Response(StatusNoContent, sessionCookieDSL)
			Response("unauthorized", StatusUnauthorized)
		})
	})

	Method("device_start", func() {
		Description("Begin signing in a client without a browser, such as the TUI. The client shows the user code and the URL, people approve the code in the web UI, signed in with a passkey, and the client gets a session from device_token meanwhile.")
		Result(func() {
			RequiredAttribute("device_code", String, "Secret code the client asks device_token with; never shown to people")
			RequiredAttribute("user_code", String, "Code people compare with the one in the web UI before approving it", func() {
				Example("BCDF-GHJK")
			})
			RequiredAttribute("verification_uri", String, "URL of the page of the web UI that approves the user code")
			RequiredAttribute("expires_in", Int, "Seconds until the request expires")
			RequiredAttribute("interval", Int, "Seconds to wait between calls of device_token")
		})
		HTTP(func() {
			POST("/auth/device")
		})
	})

	Method("device_token", func() {
		Description("Ask whether people approved the request of device_start. Once approved, the result has the session, which the client keeps in memory only and sends as the session cookie; the request is then gone.")
		Payload(func() {
			RequiredAttribute("device_code", String, "Device code from device_start")
		})
		Result(func() {
			RequiredAttribute("status", String, "pending: not approved yet; approved: the result has the session; expired: the request expired or is unknown, so begin again", func() {
				Enum("pending", "approved", "expired")
			})
			Attribute("session", String, "Session, when approved")
		})
		HTTP(func() {
			POST("/auth/device/token")
		})
	})

	Method("device_approve", func() {
		Description("Approve the request of device_start with its user code, from the web UI signed in with a passkey.")
		Payload(func() {
			Attribute("session", String, "Session cookie of the web UI")
			RequiredAttribute("user_code", String, "User code shown by the client")
		})
		Error("unauthorized", ErrorResult, "The request is not signed in with a passkey")
		Error("not_found", ErrorResult, "The user code is unknown or expired")
		HTTP(func() {
			POST("/auth/device/approve")
			Cookie("session:" + sessionCookie)
			Response(StatusNoContent)
			Response("unauthorized", StatusUnauthorized)
			Response("not_found", StatusNotFound)
		})
	})

	Method("passkeys", func() {
		Description("List the registered passkeys, for the web UI signed in with a passkey.")
		Payload(func() {
			Attribute("session", String, "Session cookie")
		})
		Result(ArrayOf(PasskeyInfo))
		Error("unauthorized", ErrorResult, "The request is not signed in with a passkey")
		HTTP(func() {
			GET("/auth/passkeys")
			Cookie("session:" + sessionCookie)
			Response("unauthorized", StatusUnauthorized)
		})
	})

	Method("delete_passkey", func() {
		Description("Delete a registered passkey, from the web UI signed in and verified with a passkey. The only passkey cannot be deleted; ebira auth reset removes every passkey.")
		Payload(func() {
			Attribute("session", String, "Session cookie")
			RequiredAttribute("id", String, "ID of the passkey")
		})
		Error("unauthorized", ErrorResult, "The request is not signed in with a passkey")
		Error("verification_required", ErrorResult, "The session is not signed in with a passkey recently: sign in again")
		Error("not_found", ErrorResult, "The passkey is not registered")
		Error("bad_request", ErrorResult, "The passkey is the only one")
		HTTP(func() {
			DELETE("/auth/passkeys/{id}")
			Cookie("session:" + sessionCookie)
			Response(StatusNoContent)
			Response("unauthorized", StatusUnauthorized)
			Response("verification_required", StatusForbidden)
			Response("not_found", StatusNotFound)
			Response("bad_request", StatusBadRequest)
		})
	})

	Method("revoke_sessions", func() {
		Description("End every session, such as those of other browsers and of the TUI, from the web UI signed in and verified with a passkey. The response sets a new session for the request.")
		Payload(func() {
			Attribute("session", String, "Session cookie")
		})
		Result(func() {
			RequiredAttribute("session", String, "New session")
		})
		Error("unauthorized", ErrorResult, "The request is not signed in with a passkey")
		Error("verification_required", ErrorResult, "The session is not signed in with a passkey recently: sign in again")
		HTTP(func() {
			POST("/auth/sessions/revoke")
			Cookie("session:" + sessionCookie)
			Response(StatusNoContent, sessionCookieDSL)
			Response("unauthorized", StatusUnauthorized)
			Response("verification_required", StatusForbidden)
		})
	})

	Method("logout", func() {
		Description("Sign out: the response clears the session cookie.")
		Result(func() {
			RequiredAttribute("session", String, "Empty session")
		})
		HTTP(func() {
			POST("/auth/logout")
			Response(StatusNoContent, sessionCookieDSL)
		})
	})
})
