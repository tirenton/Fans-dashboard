package main

import (
	"os"
	"testing"
	"time"
)

func TestRequestPullWritesOnceAndReportsPending(t *testing.T) {
	dir := t.TempDir()
	if st := ReadPull(dir); st.Pending || !st.Enabled {
		t.Fatalf("fresh dir: %+v", st)
	}
	t1 := at("2026-11-11T01:20:00Z")
	st, err := RequestPull(dir, t1)
	if err != nil || !st.Pending || st.RequestedAt == nil || !st.RequestedAt.Equal(t1) {
		t.Fatalf("first request: %+v %v", st, err)
	}
	// a second click while pending keeps the original time
	st, _ = RequestPull(dir, t1.Add(time.Minute))
	if !st.RequestedAt.Equal(t1) {
		t.Fatalf("second request changed time: %+v", st)
	}
	// the prototype deletes the file when its pass starts
	os.Remove(pullFile(dir))
	if st := ReadPull(dir); st.Pending {
		t.Fatalf("after pickup: %+v", st)
	}
}

func TestPullDisabledWithoutFolder(t *testing.T) {
	if st := ReadPull(""); st.Enabled || st.Pending {
		t.Fatalf("%+v", st)
	}
	if _, err := RequestPull("", time.Now()); err == nil {
		t.Fatal("expected error")
	}
}
