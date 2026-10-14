package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// How scripts find a running SituCraft. With --remote-control, SituCraft writes remote.json to its
// data folder: its URL and the token that reading and writing files need, readable by this user
// only (as Jupyter's runtime files are), and removes it when it stops. GET /api/remote/tools
// describes every action. Adapted from MaveScape 0.8.0 connection.go.

const connectionFileName = "remote.json"

type connectionInfo struct {
	URL     string `json:"url"`
	Token   string `json:"token"`
	Version string `json:"version"`
	PID     int    `json:"pid"`
}

// writeConnectionFile writes dir/remote.json through a temporary file renamed into place, so a
// reader never sees half a file. Returns its path.
func writeConnectionFile(dir, url, token string) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(connectionInfo{URL: url, Token: token, Version: version, PID: os.Getpid()}, "", "  ")
	if err != nil {
		return "", err
	}
	temp, err := os.CreateTemp(dir, ".remote-*.json")
	if err != nil {
		return "", err
	}
	name := temp.Name()
	if err = temp.Chmod(0o600); err == nil {
		_, err = temp.Write(append(data, '\n'))
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	path := filepath.Join(dir, connectionFileName)
	if err == nil {
		err = os.Rename(name, path)
	}
	if err != nil {
		os.Remove(name)
		return "", err
	}
	return path, nil
}

// removeConnectionFile removes the file if it still describes this SituCraft (another one started
// later with the same data folder replaces it, and keeps it).
func removeConnectionFile(path, token string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var info connectionInfo
	if json.Unmarshal(data, &info) == nil && info.Token == token {
		os.Remove(path)
	}
}

// connectionNotice is the banner line that names the file.
func connectionNotice(path string) string {
	return fmt.Sprintf("Scripts find this SituCraft (its address and token) in %s", path)
}
