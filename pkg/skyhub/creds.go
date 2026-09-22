package skyhub

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DefaultURL is the factory address of the Sky Hub admin UI.
const DefaultURL = "http://192.168.50.1/"

// Credentials holds what is needed to talk to a hub.
type Credentials struct {
	URL      string
	User     string
	Password string
}

// LoadCredentials resolves credentials from, in order of precedence:
//
//  1. SKYHUB_URL, SKYHUB_USER, SKYHUB_PASSWORD environment variables;
//  2. the file named by SKYHUB_CREDENTIALS_FILE, else ~/skyhub, containing
//     KEY=VALUE lines (USER, PASSWORD, optional URL).
//
// URL defaults to DefaultURL and User to "admin".
func LoadCredentials() (Credentials, error) {
	c := Credentials{
		URL:      os.Getenv("SKYHUB_URL"),
		User:     os.Getenv("SKYHUB_USER"),
		Password: os.Getenv("SKYHUB_PASSWORD"),
	}
	if c.Password == "" {
		path := os.Getenv("SKYHUB_CREDENTIALS_FILE")
		if path == "" {
			home, err := os.UserHomeDir()
			if err == nil {
				path = filepath.Join(home, "skyhub")
			}
		}
		if path != "" {
			fc, err := ReadCredentialsFile(path)
			if err != nil && !os.IsNotExist(err) {
				return c, err
			}
			if c.URL == "" {
				c.URL = fc.URL
			}
			if c.User == "" {
				c.User = fc.User
			}
			c.Password = fc.Password
		}
	}
	if c.URL == "" {
		c.URL = DefaultURL
	}
	if c.User == "" {
		c.User = "admin"
	}
	if c.Password == "" {
		return c, fmt.Errorf("skyhub: no password: set SKYHUB_PASSWORD or PASSWORD= in ~/skyhub")
	}
	return c, nil
}

// ReadCredentialsFile parses a KEY=VALUE file (USER, PASSWORD, URL).
func ReadCredentialsFile(path string) (Credentials, error) {
	var c Credentials
	f, err := os.Open(path)
	if err != nil {
		return c, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
			v = v[1 : len(v)-1]
		}
		switch strings.ToUpper(strings.TrimSpace(k)) {
		case "USER", "SKYHUB_USER":
			c.User = v
		case "PASSWORD", "SKYHUB_PASSWORD":
			c.Password = v
		case "URL", "SKYHUB_URL":
			c.URL = v
		}
	}
	return c, sc.Err()
}
