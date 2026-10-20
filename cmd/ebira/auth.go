package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"

	genauth "github.com/ikawaha/ebira/gen/auth"
	authcli "github.com/ikawaha/ebira/gen/http/auth/client"
	"github.com/ikawaha/ebira/internal/auth"
	"github.com/ikawaha/ebira/internal/authclient"
	"github.com/ikawaha/ebira/internal/ui/web"
	goahttp "goa.design/goa/v3/http"
)

// runAuth runs ebira auth.
func runAuth(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "passkey":
			return authPasskey(args[1:])
		case "reset":
			return authReset(args[1:], os.Stdin, os.Stdout)
		}
	}
	return fmt.Errorf("usage: ebira auth passkey|reset [flags]")
}

// authReset runs ebira auth reset: it removes the passkeys and ends every
// session, for when the passkeys are lost. The server reads the passkeys on
// every request, so it needs no restart.
func authReset(args []string, in io.Reader, out io.Writer) error {
	fs := flag.NewFlagSet("auth reset", flag.ContinueOnError)
	var (
		notesDir = fs.String("notes", defaultNotesDir(), "notes directory whose passkeys to remove")
		yes      = fs.Bool("yes", false, "remove them without asking")
	)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), `usage: ebira auth reset [flags]

Remove the registered passkeys and end every session, such as when the
passkeys are lost. Then register a passkey again with ebira auth passkey, or
stop requiring passkeys: set auth: none in the settings file and run ebira
server restart.`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	dir, err := absDir(*notesDir)
	if err != nil {
		return err
	}
	if !*yes {
		if !interactive() {
			return errors.New("ebira auth reset removes every passkey: add -yes to do it without a terminal")
		}
		ok, err := confirm(bufio.NewReader(in), out, "Remove every passkey of "+dir+" and end every session", false)
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(out, "Nothing removed.")
			return nil
		}
	}
	sd, err := stateDir(dir)
	if err != nil {
		return err
	}
	if err := auth.Reset(auth.Dir(sd)); err != nil {
		return err
	}
	fmt.Fprintln(out, "Removed the passkeys and ended every session. While the server requires passkeys (auth: passkey), register one again with ebira auth passkey; or set auth: none in the settings file and run ebira server restart.")
	return nil
}

// authPasskey runs ebira auth passkey: it opens the page to register a
// passkey, through a one-time URL, so that only people at the terminal can
// register passkeys.
func authPasskey(args []string) error {
	fs := flag.NewFlagSet("auth passkey", flag.ContinueOnError)
	var (
		notesDir = fs.String("notes", defaultNotesDir(), "notes directory whose server registers the passkey, started in the background if needed")
		addr     = fs.String("addr", defaultAddr(), "address of a server started for the registration")
		noOpen   = fs.Bool("no-open", false, "only print the URL, without opening the browser")
	)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), `usage: ebira auth passkey [flags]

Register a passkey for the web UI: open a one-time URL, valid for 10
minutes, in the browser, and create the passkey there. The server requires
passkeys with -auth=passkey or auth: passkey in the settings file; then the
REST API serves only requests signed in with a passkey, as from people, and
refuses the others. MCP stays open to AI clients.`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	ctx := context.Background()
	dir, err := notesDirOf(fs, *notesDir, interactive(), os.Stdin, os.Stdout)
	if err != nil {
		return err
	}
	a, err := ensureServer(ctx, dir, *addr, os.Stdin, os.Stdout)
	if err != nil {
		return err
	}
	_, port, err := net.SplitHostPort(a)
	if err != nil {
		return err
	}
	sd, err := stateDir(dir)
	if err != nil {
		return err
	}
	token, err := auth.NewRegisterToken(auth.Dir(sd))
	if err != nil {
		return err
	}
	// Passkeys belong to localhost, whatever address the server listens on.
	u := "http://localhost:" + port + web.Prefix + "#register/" + token
	fmt.Println("Register a passkey at (once, within 10 minutes):", u)
	if st, err := authStatus(ctx, "http://"+a); err == nil && st.Mode != auth.ModePasskey {
		fmt.Println("The server does not require passkeys yet: after registering, set auth: passkey in the settings file and run ebira server restart, or run ebira server restart -auth=passkey.")
	}
	if *noOpen {
		return nil
	}
	return openBrowser(u)
}

// authAPI returns the client of the auth service of the server at u, over
// hc.
func authAPI(u *url.URL, hc *http.Client) *genauth.Client {
	c := authcli.NewClient(u.Scheme, u.Host, hc, goahttp.RequestEncoder, goahttp.ResponseDecoder, false)
	return genauth.NewClient(c.Status(), c.RegisterBegin(), c.RegisterFinish(), c.LoginBegin(), c.LoginFinish(), c.DeviceStart(), c.DeviceToken(), c.DeviceApprove(), c.Passkeys(), c.DeletePasskey(), c.RevokeSessions(), c.Logout())
}

// authStatus asks the server at base how it authenticates people.
func authStatus(ctx context.Context, base string) (*genauth.StatusResult, error) {
	u, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	return authAPI(u, http.DefaultClient).Status(ctx, &genauth.StatusPayload{})
}

// signedInClient returns the HTTP client for the server at u. While the
// server requires a passkey, it signs in first through the web UI (see
// internal/authclient), telling people what to approve on stderr; the
// session is kept in memory only, so each command signs in again.
func signedInClient(ctx context.Context, u *url.URL) (*http.Client, error) {
	cookie := authclient.NewCookie()
	hc := &http.Client{Transport: cookie}
	session, err := authclient.SignIn(ctx, authAPI(u, hc), os.Stderr)
	if err != nil {
		return nil, err
	}
	if session != "" {
		cookie.Set(session)
	}
	return hc, nil
}
