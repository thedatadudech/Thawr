// Package policy loads the YAML ACL file. In spec 001 it validates only
// the document shape and version; spec 006 adds the full parser,
// validator and compiler behind the same Load entry point.
package policy

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"gopkg.in/yaml.v3"
)

// Version is the only policy file version this binary understands.
const Version = 1

// ErrNotFound is returned by Load when the file does not exist; callers
// treat it as an empty (default deny) policy with a warning.
var ErrNotFound = errors.New("policy: file not found")

// Policy is the loaded ACL document. Only Version is interpreted yet.
type Policy struct {
	Version int `yaml:"version"`
	// Groups, TagOwners and ACLs are parsed structurally so a malformed
	// file fails at startup; their semantics arrive with spec 006.
	Groups    map[string][]string `yaml:"groups"`
	TagOwners map[string][]string `yaml:"tagOwners"`
	ACLs      []Rule              `yaml:"acls"`
}

// Rule is one accept rule.
type Rule struct {
	Action string   `yaml:"action"`
	Src    []string `yaml:"src"`
	Dst    []string `yaml:"dst"`
	Proto  string   `yaml:"proto"`
}

// Empty returns the default-deny policy used when no file exists.
func Empty() *Policy { return &Policy{Version: Version} }

// Load reads and parses the policy file at path.
func Load(path string) (*Policy, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, path)
	}
	if err != nil {
		return nil, fmt.Errorf("policy: read %s: %w", path, err)
	}
	return Parse(data)
}

// Parse decodes a policy document.
func Parse(data []byte) (*Policy, error) {
	var p Policy
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&p); err != nil {
		return nil, fmt.Errorf("policy: parse: %w", err)
	}
	if p.Version != Version {
		return nil, fmt.Errorf("policy: version %d unsupported (want %d)", p.Version, Version)
	}
	for i, r := range p.ACLs {
		if r.Action != "accept" {
			return nil, fmt.Errorf("policy: acls[%d].action %q must be accept", i, r.Action)
		}
	}
	return &p, nil
}
