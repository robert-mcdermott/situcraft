package simulate

import (
	"os"
	"testing"
)

// tempRoot holds what the tests make and share; it is removed when they finish.
var tempRoot string

func testTempDir() (string, error) { return tempRoot, nil }

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "situcraft-simulate-test-")
	if err != nil {
		panic(err)
	}
	tempRoot = dir
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
