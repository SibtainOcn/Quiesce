package main

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows/registry"
)

func TestPathHas(t *testing.T) {
	list := `C:\Windows;C:\Tools\Quiesce\;%USERPROFILE%\bin; ;"C:\Quoted Dir"`
	for _, dir := range []string{`c:\tools\quiesce`, `C:\Tools\Quiesce\`, `C:\Quoted Dir`,
		filepath.Join(os.Getenv("USERPROFILE"), "bin")} {
		if !pathHas(list, dir) {
			t.Errorf("%q not found", dir)
		}
	}
	if pathHas(list, `C:\Tools`) || pathHas("", `C:\Tools`) {
		t.Error("false match")
	}
}

func TestAppendPath(t *testing.T) {
	cases := map[string]string{
		"":          `C:\qc`,
		`C:\a`:      `C:\a;C:\qc`,
		`C:\a;`:     `C:\a;C:\qc`,
		` C:\a;; `:  `C:\a;C:\qc`,
		`C:\a;C:\b`: `C:\a;C:\b;C:\qc`,
	}
	for in, want := range cases {
		if got := appendPath(in, `C:\qc`); got != want {
			t.Errorf("appendPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAddToPathKey(t *testing.T) {
	const key = `Software\QuiesceTest\Environment`
	defer func() {
		_ = registry.DeleteKey(registry.CURRENT_USER, key)
		_ = registry.DeleteKey(registry.CURRENT_USER, `Software\QuiesceTest`)
	}()
	k, _, err := registry.CreateKey(registry.CURRENT_USER, key, registry.SET_VALUE)
	if err != nil {
		t.Skip("cannot create test key:", err)
	}
	_ = k.SetStringValue("Path", `C:\One;C:\Two;`)
	k.Close()

	dir := `C:\Tools\Quiesce Test`
	if res, err := addToPathKey(registry.CURRENT_USER, key, dir); err != nil || res != pathAdded {
		t.Fatalf("first add: %v %v", res, err)
	}
	got, typ := readPathValue(registry.CURRENT_USER, key)
	if got != `C:\One;C:\Two;C:\Tools\Quiesce Test` || typ != registry.SZ {
		t.Errorf("value %q type %d", got, typ)
	}
	if res, err := addToPathKey(registry.CURRENT_USER, key, dir+`\`); err != nil || res != pathAlready {
		t.Errorf("second add: %v %v", res, err)
	}
	if _, err := addToPathKey(registry.CURRENT_USER, key, filepath.Join(os.TempDir(), "qc")); err != errTempDir {
		t.Errorf("temp dir: %v", err)
	}
}

func TestInTempDir(t *testing.T) {
	if !inTempDir(os.TempDir()) || !inTempDir(filepath.Join(os.TempDir(), "x", "y")) {
		t.Error("temp folder not detected")
	}
	if inTempDir(`C:\Tools\Quiesce`) {
		t.Error("normal folder treated as temp")
	}
}
