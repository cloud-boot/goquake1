// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// helperRoot writes a small static tree (index.html + quake.wasm) to
// a t.TempDir + returns its absolute path. Tests use it to drive the
// real serve() loop end-to-end without depending on the engine repo's
// real wasm artefact (which is built at task time, not test time).
func helperRoot(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	if err := os.WriteFile(filepath.Join(d, "index.html"), []byte("<html><body>hi</body></html>"), 0o644); err != nil {
		t.Fatalf("write index.html: %v", err)
	}
	if err := os.WriteFile(filepath.Join(d, "quake.wasm"), []byte("\x00asm\x01\x00\x00\x00"), 0o644); err != nil {
		t.Fatalf("write quake.wasm: %v", err)
	}
	return d
}

func TestServe_RootMissing(t *testing.T) {
	err := serve("127.0.0.1:0", filepath.Join(t.TempDir(), "nope"), false, io.Discard)
	if err == nil {
		t.Fatal("serve(missing root): want error, got nil")
	}
	if !strings.Contains(err.Error(), "root ") {
		t.Fatalf("serve(missing root): err = %v want 'root '-prefixed", err)
	}
}

func TestServe_AbsRootError(t *testing.T) {
	muStartup.Lock()
	defer muStartup.Unlock()
	defer func(orig func(network, addr string) (net.Listener, error)) { netListen = orig }(netListen)
	myErr := errors.New("listen boom")
	netListen = func(network, addr string) (net.Listener, error) { return nil, myErr }

	err := serve("127.0.0.1:0", helperRoot(t), false, io.Discard)
	if err == nil || !errors.Is(err, myErr) {
		t.Fatalf("serve(listen err): got %v want listen boom", err)
	}
}

func TestServe_FilepathAbsError(t *testing.T) {
	muStartup.Lock()
	defer muStartup.Unlock()
	defer func(orig func(string) (string, error)) { filepathAbs = orig }(filepathAbs)
	myErr := errors.New("abs boom")
	filepathAbs = func(string) (string, error) { return "", myErr }

	err := serve("127.0.0.1:0", t.TempDir(), false, io.Discard)
	if err == nil || !errors.Is(err, myErr) {
		t.Fatalf("serve(abs err): got %v want abs boom", err)
	}
}

func TestServe_StatError(t *testing.T) {
	muStartup.Lock()
	defer muStartup.Unlock()
	defer func(orig func(string) (os.FileInfo, error)) { osStat = orig }(osStat)
	myErr := errors.New("stat boom")
	osStat = func(string) (os.FileInfo, error) { return nil, myErr }

	err := serve("127.0.0.1:0", t.TempDir(), false, io.Discard)
	if err == nil || !errors.Is(err, myErr) {
		t.Fatalf("serve(stat err): got %v want stat boom", err)
	}
}

func TestServe_OneShotServesAndExits(t *testing.T) {
	muStartup.Lock()
	defer muStartup.Unlock()

	defer func(orig time.Duration) { oneShotDrainDelay = orig }(oneShotDrainDelay)
	oneShotDrainDelay = 5 * time.Millisecond

	root := helperRoot(t)
	// Pre-allocate an ephemeral port so the goroutine + the client
	// race-free agree on the URL.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("port probe: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	var buf bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- serve(addr, root, true, &buf)
	}()

	// Wait for the listener to come up.
	deadline := time.Now().Add(2 * time.Second)
	var resp *http.Response
	for time.Now().Before(deadline) {
		resp, err = http.Get("http://" + addr + "/index.html")
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("GET /index.html: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if !strings.Contains(string(body), "hi") {
		t.Fatalf("body = %q want 'hi'", string(body))
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve returned %v want nil after one-shot", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("serve did not exit after one-shot")
	}

	if !strings.Contains(buf.String(), "serve-wasm: open http://") {
		t.Fatalf("startup banner missing: %q", buf.String())
	}
}

func TestServe_OneShotFaviconDoesNotTrigger(t *testing.T) {
	muStartup.Lock()
	defer muStartup.Unlock()
	defer func(orig time.Duration) { oneShotDrainDelay = orig }(oneShotDrainDelay)
	oneShotDrainDelay = 5 * time.Millisecond

	root := helperRoot(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("port probe: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	done := make(chan error, 1)
	go func() {
		done <- serve(addr, root, true, io.Discard)
	}()

	// Wait for liveness, then poke /favicon.ico -- should NOT trigger
	// shutdown. Follow up with a real request to make the server exit.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + addr + "/favicon.ico")
		if err == nil {
			_ = resp.Body.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Server should still be up. Hit / to drive the real shutdown.
	resp, err := http.Get("http://" + addr + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	_ = resp.Body.Close()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve returned %v want nil", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("serve did not exit after non-favicon hit")
	}
}

func TestWasmContentType_SetsMime(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("\x00asm"))
	})
	h := wasmContentType(inner)
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/quake.wasm")
	if err != nil {
		t.Fatalf("GET .wasm: %v", err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "application/wasm" {
		t.Fatalf("Content-Type = %q want application/wasm", ct)
	}
}

func TestWasmContentType_LeavesOtherMimeAlone(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("hi"))
	})
	h := wasmContentType(inner)
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/index.html")
	if err != nil {
		t.Fatalf("GET .html: %v", err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type = %q want text/plain prefix", ct)
	}
}

func TestMain_RunsServeBranchAndExitsOneShot(t *testing.T) {
	muStartup.Lock()
	defer muStartup.Unlock()
	defer func(orig time.Duration) { oneShotDrainDelay = orig }(oneShotDrainDelay)
	oneShotDrainDelay = 5 * time.Millisecond

	root := helperRoot(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("port probe: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	t.Setenv("QUAKE_WASM_ADDR", addr)
	t.Setenv("QUAKE_WASM_ROOT", root)
	t.Setenv("QUAKE_WASM_ONESHOT", "1")

	done := make(chan struct{})
	go func() {
		main()
		close(done)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + addr + "/index.html")
		if err == nil {
			_ = resp.Body.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("main() did not return after one-shot")
	}
}

func TestMain_OsExitOnServeFailure(t *testing.T) {
	muStartup.Lock()
	defer muStartup.Unlock()
	defer func(orig func(int)) { osExit = orig }(osExit)
	var exitCode int32 = -1
	osExit = func(code int) {
		atomic.StoreInt32(&exitCode, int32(code))
	}

	t.Setenv("QUAKE_WASM_ADDR", "127.0.0.1:0")
	t.Setenv("QUAKE_WASM_ROOT", filepath.Join(t.TempDir(), "no-such-root"))
	t.Setenv("QUAKE_WASM_ONESHOT", "")

	main()

	if atomic.LoadInt32(&exitCode) != 1 {
		t.Fatalf("osExit code = %d want 1", exitCode)
	}
}

func TestServe_NonGracefulError(t *testing.T) {
	muStartup.Lock()
	defer muStartup.Unlock()
	defer func(orig func(network, addr string) (net.Listener, error)) { netListen = orig }(netListen)

	// Inject a listener that's already closed so srv.Serve returns
	// "use of closed network connection" -- the non-ErrServerClosed
	// branch.
	netListen = func(network, addr string) (net.Listener, error) {
		ln, err := net.Listen(network, "127.0.0.1:0")
		if err != nil {
			return nil, err
		}
		_ = ln.Close()
		return ln, nil
	}
	err := serve("ignored", helperRoot(t), false, io.Discard)
	if err == nil {
		t.Fatal("serve(closed listener): want error, got nil")
	}
	if errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("serve(closed listener): mapped to nil when it shouldn't: %v", err)
	}
}

func TestMain_DefaultEnvsApplied(t *testing.T) {
	muStartup.Lock()
	defer muStartup.Unlock()
	// Force defaults: clear all three env vars + swap osExit so the
	// "default root doesn't exist" branch returns without process exit.
	t.Setenv("QUAKE_WASM_ADDR", "")
	t.Setenv("QUAKE_WASM_ROOT", "")
	t.Setenv("QUAKE_WASM_ONESHOT", "")
	defer func(orig func(int)) { osExit = orig }(osExit)
	var exitCode int32 = -1
	osExit = func(code int) {
		atomic.StoreInt32(&exitCode, int32(code))
	}
	// CWD probably doesn't contain cmd/quake-wasm/web -> serve()
	// returns the "root ... no such file" error -> osExit(1).
	main()
	if atomic.LoadInt32(&exitCode) != 1 {
		t.Fatalf("osExit code = %d want 1 (default root missing under test cwd)", exitCode)
	}
}
