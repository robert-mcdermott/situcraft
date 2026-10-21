package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var updateClients = flag.Bool("update", false, "rewrite clients/tools.json from the actions (then run node clients/generate.mjs)")

// clients/tools.json is the list of actions the R and Python clients are generated from
// (clients/generate.mjs; wave 9, slice 7); it must describe the actions this program has. After
// MaveScape 0.8.0's clients_test.go.
func TestTheClientsToolListIsCurrent(t *testing.T) {
	data, err := json.MarshalIndent(remoteActions, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	path := filepath.Join("clients", "tools.json")
	if *updateClients {
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	current, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(current, data) {
		t.Fatalf("clients/tools.json is out of date: run go test -run TestTheClientsToolListIsCurrent -update, then node clients/generate.mjs")
	}
}
