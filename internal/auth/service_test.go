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

	device, code, _ := p.StartDevice()
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
	_, code2, _ := p.StartDevice()
	if err := svc.DeviceApprove(ctx, &genauth.DeviceApprovePayload{Session: &s, UserCode: code2}); errName(err) != "verification_required" {
		t.Errorf("approval with the session of a client = %v, want verification_required", err)
	}
}
