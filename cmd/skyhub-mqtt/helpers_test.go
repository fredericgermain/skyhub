package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fredericgermain/skyhub/pkg/skyhubtest"
)

func readFixture(t *testing.T, page string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(fixtureDir, skyhubtest.FixtureName(page)))
	if err != nil {
		t.Fatal(err)
	}
	return b
}
