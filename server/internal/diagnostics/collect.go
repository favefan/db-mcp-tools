package diagnostics

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type LogFile struct {
	Name         string            `json:"name"`
	Records      []json.RawMessage `json:"records"`
	Truncated    bool              `json:"truncated"`
	SkippedLines int               `json:"skipped_lines"`
}
type Logs struct {
	Directory string    `json:"directory"`
	Files     []LogFile `json:"files"`
	Issues    []string  `json:"issues"`
	Truncated bool      `json:"truncated"`
}

// Collect never exports arbitrary files, legacy stderr, credentials or SQLite.
// Newest files are preferred; every size/record omission is explicit.
func Collect(dir string) Logs {
	out := Logs{Directory: dir, Files: []LogFile{}, Issues: []string{}}
	info, err := os.Lstat(dir)
	if os.IsNotExist(err) {
		out.Issues = append(out.Issues, "log directory does not exist")
		return out
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		out.Issues = append(out.Issues, "log directory unavailable or not regular")
		return out
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		out.Issues = append(out.Issues, "cannot list log directory")
		return out
	}
	type candidate struct {
		name     string
		modified time.Time
	}
	files := []candidate{}
	for _, entry := range entries {
		if logName.MatchString(entry.Name()) {
			if i, e := entry.Info(); e == nil && i.Mode().IsRegular() {
				files = append(files, candidate{entry.Name(), i.ModTime()})
			}
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].modified.After(files[j].modified) })
	remaining := int64(8 << 20)
	for _, file := range files {
		if remaining <= 0 || len(out.Files) >= 16 {
			out.Truncated = true
			break
		}
		b, e := ReadRegular(filepath.Join(dir, file.name), MaxFileBytes+1)
		if e != nil {
			out.Issues = append(out.Issues, "cannot read "+file.name)
			continue
		}
		f := LogFile{Name: file.name, Records: []json.RawMessage{}}
		if int64(len(b)) > remaining {
			b = b[:remaining]
			f.Truncated = true
		}
		if len(b) > MaxFileBytes {
			b = b[:MaxFileBytes]
			f.Truncated = true
		}
		remaining -= int64(len(b))
		scan := bufio.NewScanner(bytes.NewReader(b))
		scan.Buffer(make([]byte, 4096), 64<<10)
		for scan.Scan() {
			var v map[string]any
			if json.Unmarshal(scan.Bytes(), &v) != nil || v["component"] == nil || v["msg"] == nil {
				f.SkippedLines++
				continue
			}
			safe, _ := json.Marshal(Sanitize(v))
			f.Records = append(f.Records, safe)
		}
		if scan.Err() != nil {
			f.Truncated = true
			out.Issues = append(out.Issues, "invalid or oversized line in "+file.name)
		}
		out.Truncated = out.Truncated || f.Truncated
		out.Files = append(out.Files, f)
	}
	return out
}
