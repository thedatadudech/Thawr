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

func TestSubcommandsWithoutActionShowHelp(t *testing.T) {
	for _, name := range []string{"client", "admin", "admin user", "admin token", "admin peer"} {
		t.Run(name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			root := newRootCmd(&out, &errOut)
			root.SetArgs(strings.Fields(name))
			if err := root.Execute(); err != nil {
				t.Fatalf("got %v, want help without error", err)
			}
			if !strings.Contains(out.String(), "Usage:") {
				t.Errorf("no usage printed:\n%s", out.String())
			}
		})
	}
}

func TestClientUpRequiresFlags(t *testing.T) {
	var out, errOut bytes.Buffer
	root := newRootCmd(&out, &errOut)
	root.SetArgs([]string{"client", "up"})
	err := root.Execute()
	var ee *exitError
	if !errors.As(err, &ee) || ee.code != exitConfigError {
		t.Fatalf("got %v, want exit code %d", err, exitConfigError)
	}
}
