package main

import (
	"bytes"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunAllRunsEveryCheckConcurrentlyAndKeepsOrder(t *testing.T) {
	items := []check{{name: "first"}, {name: "second"}, {name: "third"}}
	var calls atomic.Int32
	started := make(chan struct{}, len(items))
	release := make(chan struct{})
	done := make(chan []result)

	go func() {
		done <- runAll(items, func(item check) result {
			calls.Add(1)
			started <- struct{}{}
			<-release
			return result{check: item}
		})
	}()
	for range items {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("checks did not start concurrently")
		}
	}
	close(release)
	results := <-done

	if calls.Load() != int32(len(items)) {
		t.Fatalf("ran %d checks, want %d", calls.Load(), len(items))
	}
	for i := range items {
		if results[i].check.name != items[i].name {
			t.Fatalf("result %d is %q, want %q", i, results[i].check.name, items[i].name)
		}
	}
}

func TestReportSuccess(t *testing.T) {
	var stdout, stderr bytes.Buffer

	if !report(&stdout, &stderr, []result{{check: check{name: "pass"}}}) {
		t.Fatal("report returned failure")
	}
	if stdout.String() != "ok\n" {
		t.Fatalf("stdout = %q, want %q", stdout.String(), "ok\\n")
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestReportIncludesEveryFailure(t *testing.T) {
	results := []result{
		{check: check{label: "first"}, output: []byte("first output\n"), err: errors.New("failed")},
		{check: check{name: "pass"}},
		{check: check{label: "second"}, output: []byte("second output"), err: errors.New("failed too")},
	}
	var stdout, stderr bytes.Buffer

	if report(&stdout, &stderr, results) {
		t.Fatal("report returned success")
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	for _, want := range []string{"==> first: failed\n", "first output\n", "==> second: failed too\n", "second output\n"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr = %q, want it to contain %q", stderr.String(), want)
		}
	}
}
