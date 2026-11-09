package main

import (
	"encoding/base64"
	"testing"
)

func TestDownloadsAdd(t *testing.T) {
	var d downloads
	enc := base64.StdEncoding.EncodeToString
	if b, err := d.add("a", enc([]byte("hello, ")), false); b != nil || err != nil {
		t.Fatalf("add a first part = %q, %v", b, err)
	}
	if b, err := d.add("b", enc([]byte("other")), true); string(b) != "other" || err != nil {
		t.Errorf("add the only part of b = %q, %v", b, err)
	}
	if b, err := d.add("a", enc([]byte("world")), true); string(b) != "hello, world" || err != nil {
		t.Errorf("add the last part of a = %q, %v", b, err)
	}
	if len(d.files) != 0 {
		t.Errorf("files left: %v", d.files)
	}

	if _, err := d.add("c", "not base64!", true); err == nil {
		t.Error("add a part not in base64: no error")
	}
	big := enc(make([]byte, maxDownload/2+1))
	d.add("d", big, false)
	if _, err := d.add("d", big, false); err == nil {
		t.Error("add more than maxDownload: no error")
	}
	if len(d.files) != 0 {
		t.Errorf("files left after errors: %v", d.files)
	}
}
