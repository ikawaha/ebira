//go:build unix

package daemon

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLock(t *testing.T) {
	dir := t.TempDir()
	if running, err := Running(dir); err != nil || running {
		t.Fatalf("Running before the lock = %v, %v", running, err)
	}
	f, err := Lock(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Lock(dir); !errors.Is(err, ErrRunning) {
		t.Errorf("second Lock = %v, want ErrRunning", err)
	}
	if running, err := Running(dir); err != nil || !running {
		t.Errorf("Running while locked = %v, %v", running, err)
	}
	f.Close()
	if running, err := Running(dir); err != nil || running {
		t.Errorf("Running after unlocking = %v, %v", running, err)
	}
}

func TestStateAndHealth(t *testing.T) {
	dir := t.TempDir()
	s := State{PID: os.Getpid(), Addr: "localhost:8080", Version: Version(), NotesDir: dir, StartedAt: time.Now().UTC().Truncate(time.Second), Flags: []string{"-addr=localhost:8080", "-watch=false"}}
	if err := WriteState(dir, s); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadState(dir); err != nil || !reflect.DeepEqual(got, s) {
		t.Errorf("ReadState = %+v, %v", got, err)
	}
	srv := httptest.NewServer(HealthHandler(s))
	defer srv.Close()
	got, err := Health(context.Background(), strings.TrimPrefix(srv.URL, "http://"))
	if err != nil || !reflect.DeepEqual(got, s) {
		t.Errorf("Health = %+v, %v", got, err)
	}
	if err := RemoveState(dir); err != nil {
		t.Fatal(err)
	}
	if err := RemoveState(dir); err != nil {
		t.Errorf("RemoveState of a missing file: %v", err)
	}
	if len(Version()) != 12 {
		t.Errorf("Version = %q", Version())
	}
	if !Alive(os.Getpid()) || Alive(-1) {
		t.Error("Alive")
	}
}
