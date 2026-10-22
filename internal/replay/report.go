package replay

import (
	_ "embed"
	"encoding/json"
	"html"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mxygem/jam/internal/chart"
)

//go:embed report.html
var reportPage string

// WriteReport writes the run report: one self-contained HTML page with the
// run's visuals and, when h is given, the player's history on the part.
func WriteReport(w io.Writer, v *RunView, h *History) error {
	data, err := json.Marshal(struct {
		Run     *RunView `json:"run"`
		History *History `json:"history"`
	}{v, h})
	if err != nil {
		return err
	}
	// json.Marshal already escapes <, > and &, so the data can't close the
	// script tag it sits in.
	page := strings.Replace(reportPage, "{{TITLE}}", html.EscapeString(v.Song+" · run report"), 1)
	page = strings.Replace(page, "{{DATA}}", string(data), 1)
	_, err = io.WriteString(w, page)
	return err
}

// SaveReport writes the report to path, creating its folder.
func SaveReport(path string, v *RunView, h *History) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := WriteReport(f, v, h); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// ReportName is the report file name for a recording file.
func ReportName(runPath string) string {
	base := filepath.Base(runPath)
	base = strings.TrimSuffix(base, ".gz")
	base = strings.TrimSuffix(base, ".json")
	base = strings.TrimSuffix(base, ".jamrun")
	if base == "" || base == "." {
		base = "run"
	}
	return base + ".html"
}

// BuildReport gathers a run's view and its player's history on the part.
// full is the whole part (BuildReport takes the late joiner's tail
// itself); runsDir is where to look for the other attempts; owns picks the
// player's runs by profile id. rec is the run at runPath (runPath may be
// empty for a run that wasn't saved).
func BuildReport(rec *Recording, runPath string, c *chart.Chart, full *chart.Track, chartHash, runsDir string, owns func(profile string) bool) (*RunView, *History) {
	v := BuildRunView(rec, c, TrackFor(rec, full), nil)
	if a, err := filepath.Abs(runsDir); err == nil {
		runsDir = a
	}
	paths := FindRuns(runsDir, rec.Song.Dir, rec.Instrument, rec.Difficulty, owns)
	loaded := map[string]*Recording{}
	if runPath != "" {
		if a, err := filepath.Abs(runPath); err == nil {
			runPath = a
		}
		found := false
		for i, p := range paths {
			if strings.EqualFold(filepath.Clean(p), filepath.Clean(runPath)) {
				paths[i] = runPath
				found = true
			}
		}
		loaded[runPath] = rec
		if !found {
			paths = append(paths, runPath)
			sort.SliceStable(paths, func(i, j int) bool { return filepath.Base(paths[i]) < filepath.Base(paths[j]) })
		}
	}
	return v, BuildHistory(paths, c, full, chartHash, runPath, loaded)
}
