//go:build windows

package mux

import (
	"bytes"
	"testing"
)

func TestTranslateKeySpecials(t *testing.T) {
	cases := map[string][]byte{
		"Enter":  {'\r'},
		"BSpace": {0x7f},
		"Tab":    {'\t'},
		"ESC":    {0x1b},
		"Space":  {' '},
		"Up":     []byte("\x1b[A"),
		"Down":   []byte("\x1b[B"),
		"Left":   []byte("\x1b[D"),
		"Right":  []byte("\x1b[C"),
	}
	for token, want := range cases {
		if got := translateKey(token); !bytes.Equal(got, want) {
			t.Errorf("translateKey(%q) = %q, want %q", token, got, want)
		}
	}
}

func TestTranslateKeyCtrl(t *testing.T) {
	cases := map[string]byte{
		"C-c": 0x03,
		"C-d": 0x04,
		"C-a": 0x01,
		"C-Z": 0x1a,
	}
	for token, want := range cases {
		got := translateKey(token)
		if len(got) != 1 || got[0] != want {
			t.Errorf("translateKey(%q) = %v, want 0x%02x", token, got, want)
		}
	}
}

func TestTranslateKeyLiteralFallback(t *testing.T) {
	got := translateKey("hello")
	if string(got) != "hello" {
		t.Errorf("literal token = %q, want %q", got, "hello")
	}
}

func TestTranslateKeysConcat(t *testing.T) {
	got := translateKeys([]string{"hello", "Enter"})
	if string(got) != "hello\r" {
		t.Errorf("concat = %q, want %q", got, "hello\r")
	}
}
