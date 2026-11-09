package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	genauth "github.com/ikawaha/ebira/gen/auth"
	goa "goa.design/goa/v3/pkg"
)

func errName(err error) string {
	var se *goa.ServiceError
	if errors.As(err, &se) {
		return se.Name
	}
	return ""
}

// Approving a client needs a session verified with the passkey recently, so
// a session that is not, such as one a client got this way, cannot approve
// another and renew itself.
func TestDeviceApproveVerified(t *testing.T) {
	ctx := context.Background()
	p, _ := newTest(t)
	register(t, p)
	svc := NewService(ModePasskey, p)
	hour := time.Now().Add(time.Hour)

	device, code, _ := p.StartDevice("")
	unverified := session(t, p, hour, time.Time{})
	if err := svc.DeviceApprove(ctx, &genauth.DeviceApprovePayload{Session: &unverified, UserCode: code}); errName(err) != "verification_required" {
		t.Errorf("approval with a session not verified = %v, want verification_required", err)
	}
	if st, _ := p.PollDevice(device); st == DeviceApproved {
		t.Error("the request was approved")
	}

	verified := session(t, p, hour, time.Now().Add(time.Minute))
	if err := svc.DeviceApprove(ctx, &genauth.DeviceApprovePayload{Session: &verified, UserCode: code}); err != nil {
		t.Errorf("approval with a verified session: %v", err)
	}
	st, s := p.PollDevice(device)
	if st != DeviceApproved || s == "" {
		t.Fatalf("poll after the approval = %s, %q", st, s)
	}
	// The session of the client is not verified, so it cannot approve.
	_, code2, _ := p.StartDevice("")
	if err := svc.DeviceApprove(ctx, &genauth.DeviceApprovePayload{Session: &s, UserCode: code2}); errName(err) != "verification_required" {
		t.Errorf("approval with the session of a client = %v, want verification_required", err)
	}
}

// People signed in see when a request was made and by what before they
// approve it; others see nothing of it.
func TestDeviceRequest(t *testing.T) {
	ctx := context.Background()
	p, _ := newTest(t)
	register(t, p)
	svc := NewService(ModePasskey, p)
	before := time.Now().Add(-time.Second)
	started, err := svc.DeviceStart(ctx, &genauth.DeviceStartPayload{Client: ptr("tui")})
	if err != nil {
		t.Fatal(err)
	}
	signedIn := session(t, p, time.Now().Add(time.Hour), time.Time{})

	r, err := svc.DeviceRequest(ctx, &genauth.DeviceRequestPayload{Session: &signedIn, UserCode: started.UserCode})
	if err != nil {
		t.Fatal(err)
	}
	created, err := time.Parse(time.RFC3339, r.CreatedAt)
	if err != nil || created.Before(before.Truncate(time.Second)) || created.After(time.Now()) {
		t.Errorf("created_at = %s, %v", r.CreatedAt, err)
	}
	if r.Client == nil || *r.Client != "tui" || r.ExpiresIn <= 0 {
		t.Errorf("request = %+v, want the tui and a time left", r)
	}

	if _, err := svc.DeviceRequest(ctx, &genauth.DeviceRequestPayload{UserCode: started.UserCode}); errName(err) != "unauthorized" {
		t.Errorf("without a session = %v, want unauthorized", err)
	}
	if _, err := svc.DeviceRequest(ctx, &genauth.DeviceRequestPayload{Session: &signedIn, UserCode: "BCDF-GHJK"}); errName(err) != "not_found" {
		t.Errorf("an unknown code = %v, want not_found", err)
	}
	// A client that does not tell what it is.
	other, err := svc.DeviceStart(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r, err := svc.DeviceRequest(ctx, &genauth.DeviceRequestPayload{Session: &signedIn, UserCode: other.UserCode}); err != nil || r.Client != nil {
		t.Errorf("request of a client that does not tell = %+v, %v", r, err)
	}
}

func ptr[T any](v T) *T { return &v }
