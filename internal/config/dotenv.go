package config

import (
	"bufio"
	"os"
	"strings"
)

// loadDotEnv loads KEY=VALUE pairs from a .env file into the process
// environment. It is intentionally dependency-free and forgiving:
//
//   - A missing file is not an error (.env is optional).
//   - Blank lines and lines starting with '#' are ignored.
//   - A leading "export " prefix is stripped (shell-compat).
//   - Surrounding single or double quotes around the value are removed.
//   - An existing environment variable is NEVER overwritten, so real
//     environment variables (and anything the launcher already exported) take
//     precedence over the file. This matches standard dotenv semantics.
//
// Making the Go program load .env itself removes the previous fragility where
// a PowerShell launcher had to parse .env correctly (and remember UTF-8
// encoding) — otherwise real data-source credentials silently fell back to
// mock. Now .env is the single source of truth regardless of how the binary is
// started.
//
// The file is read as raw bytes and interpreted as UTF-8 (Go's default), so
// non-ASCII comments do not corrupt parsing.
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return // optional file; absence is fine
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// Allow long values (e.g. JWT bearer tokens) well beyond the default 64KB.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")

		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		if key == "" {
			continue
		}

		// Strip matching surrounding quotes.
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') ||
				(val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}

		// Never overwrite an existing environment variable.
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		_ = os.Setenv(key, val)
	}
}
