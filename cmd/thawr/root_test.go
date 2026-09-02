package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestRootListsSubcommands(t *testing.T) {
	var out, errOut bytes.Buffer
	root := newRootCmd(&out, &errOut)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("help: %v", err)
	}
	for _, want := range []string{"server", "client", "admin", "version"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("help output missing subcommand %q:\n%s", want, out.String())
		}
	}
}

func TestVersion(t *testing.T) {
	var out, errOut bytes.Buffer
	root := newRootCmd(&out, &errOut)
	root.SetArgs([]string{"version"})
	if err := root.Execute(); err != nil {
		t.Fatalf("version: %v", err)
	}
	if got, want := out.String(), "thawr dev\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestUnimplementedSubcommands(t *testing.T) {
	for _, name := range []string{"server", "client", "admin"} {
		t.Run(name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			root := newRootCmd(&out, &errOut)
			root.SetArgs([]string{name})
			err := root.Execute()
			if !errors.Is(err, errNotImplemented) {
				t.Fatalf("got %v, want errNotImplemented", err)
			}
		})
	}
}
