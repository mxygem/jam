package chart

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"
)

// SongIni holds the [song] section of a song.ini with lower-cased keys.
type SongIni struct {
	Values map[string]string
}

// ReadSongIni parses a song.ini file.
func ReadSongIni(path string) (*SongIni, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseSongIni(bytes.NewReader(b))
}

// ParseSongIni parses song.ini content. Keys outside a [song] section are
// accepted when the file has no section header at all.
func ParseSongIni(r io.Reader) (*SongIni, error) {
	ini := &SongIni{Values: map[string]string{}}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	section := ""
	sawSection := false
	first := true
	for sc.Scan() {
		line := sc.Text()
		if first {
			line = strings.TrimPrefix(line, "\uFEFF")
			first = false
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			sawSection = true
			continue
		}
		if sawSection && section != "song" {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.ToLower(strings.TrimSpace(k))
		v = strings.TrimSpace(v)
		if !utf8.ValidString(v) {
			v = latin1ToUTF8(v)
		}
		if k != "" {
			ini.Values[k] = v
		}
	}
	return ini, sc.Err()
}

func latin1ToUTF8(s string) string {
	r := make([]rune, 0, len(s))
	for i := 0; i < len(s); i++ {
		r = append(r, rune(s[i]))
	}
	return string(r)
}

// String returns a value or "".
func (s *SongIni) String(key string) string {
	if s == nil {
		return ""
	}
	return s.Values[strings.ToLower(key)]
}

// Int returns an integer value.
func (s *SongIni) Int(key string) (int64, bool) {
	v := s.String(key)
	if v == "" {
		return 0, false
	}
	if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		return n, true
	}
	if f, err := strconv.ParseFloat(v, 64); err == nil {
		return int64(f), true
	}
	return 0, false
}

// Float returns a decimal value.
func (s *SongIni) Float(key string) (float64, bool) {
	v := s.String(key)
	if v == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(v, 64)
	return f, err == nil
}

// Bool returns a boolean value ("true"/"1"/"yes").
func (s *SongIni) Bool(key string) (bool, bool) {
	v := strings.ToLower(s.String(key))
	switch v {
	case "true", "1", "yes", "on":
		return true, true
	case "false", "0", "no", "off":
		return false, true
	}
	return false, false
}
