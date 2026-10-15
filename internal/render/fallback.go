package render

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	gtfont "github.com/go-text/typesetting/font"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
)

// Song titles and artists come in every script. Barlow covers Latin; other
// scripts fall back to fonts already installed on the system, loaded only
// when a string first needs them (CJK fonts are 10-20 MB each).

type script uint8

const (
	scriptOther script = iota
	scriptHangul
	scriptKana
	scriptHan
	numScripts
)

func scriptOf(r rune) script {
	switch {
	case r >= 0xAC00 && r <= 0xD7AF, r >= 0x1100 && r <= 0x11FF, r >= 0x3130 && r <= 0x318F:
		return scriptHangul
	case r >= 0x3040 && r <= 0x30FF, r >= 0x31F0 && r <= 0x31FF, r >= 0xFF66 && r <= 0xFF9F:
		return scriptKana
	case r >= 0x4E00 && r <= 0x9FFF, r >= 0x3400 && r <= 0x4DBF, r >= 0x3000 && r <= 0x303F, r >= 0xFF00 && r <= 0xFF65:
		return scriptHan
	}
	return scriptOther
}

// fallbackCandidates lists font files per script, most preferred first.
func fallbackCandidates(s script) []string {
	var dirs []string
	var files map[script][]string
	switch runtime.GOOS {
	case "windows":
		win := os.Getenv("WINDIR")
		if win == "" {
			win = `C:\Windows`
		}
		dirs = []string{filepath.Join(win, "Fonts")}
		if la := os.Getenv("LOCALAPPDATA"); la != "" {
			dirs = append(dirs, filepath.Join(la, "Microsoft", "Windows", "Fonts"))
		}
		files = map[script][]string{
			scriptHangul: {"malgun.ttf", "gulim.ttc"},
			scriptKana:   {"YuGothM.ttc", "meiryo.ttc", "msgothic.ttc"},
			scriptHan:    {"msyh.ttc", "YuGothM.ttc", "msgothic.ttc", "malgun.ttf"},
			scriptOther:  {"segoeui.ttf", "seguisym.ttf", "Nirmala.ttf", "seguihis.ttf", "arial.ttf", "seguiemj.ttf"},
		}
	case "darwin":
		dirs = []string{"/System/Library/Fonts", "/System/Library/Fonts/Supplemental", "/Library/Fonts"}
		files = map[script][]string{
			scriptHangul: {"AppleSDGothicNeo.ttc"},
			scriptKana:   {"Hiragino Sans GB.ttc", "ヒラギノ角ゴシック W3.ttc"},
			scriptHan:    {"Hiragino Sans GB.ttc", "STHeiti Medium.ttc"},
			scriptOther:  {"Arial Unicode.ttf", "Helvetica.ttc", "Apple Symbols.ttf"},
		}
	default:
		dirs = []string{"/usr/share/fonts/opentype/noto", "/usr/share/fonts/noto-cjk", "/usr/share/fonts/truetype/noto", "/usr/share/fonts/truetype/dejavu", "/usr/share/fonts/TTF", "/usr/share/fonts/noto"}
		cjk := []string{"NotoSansCJK-Regular.ttc", "NotoSansCJK-Medium.ttc", "NotoSansCJKjp-Regular.otf"}
		files = map[script][]string{
			scriptHangul: cjk, scriptKana: cjk, scriptHan: cjk,
			scriptOther: {"NotoSans-Regular.ttf", "DejaVuSans.ttf"},
		}
	}
	var out []string
	for _, f := range files[s] {
		for _, d := range dirs {
			out = append(out, filepath.Join(d, f))
		}
	}
	return out
}

type runeCover interface {
	NominalGlyph(r rune) (gtfont.GID, bool)
}

func covers(src *text.GoTextFaceSource, r rune) bool {
	if f, ok := src.UnsafeInternal().(runeCover); ok {
		_, has := f.NominalGlyph(r)
		return has
	}
	return false
}

type fallbacks struct {
	mu      sync.Mutex
	loaded  map[string]*text.GoTextFaceSource // path -> source (nil when it failed)
	pick    map[rune]*text.GoTextFaceSource   // rune -> source that has it (nil: none)
	primary map[rune]bool                     // rune covered by Barlow
	multi   map[multiKey]*text.MultiFace
}

type multiKey struct {
	font  Font
	size  int
	chain string
}

var fb = &fallbacks{
	loaded:  map[string]*text.GoTextFaceSource{},
	pick:    map[rune]*text.GoTextFaceSource{},
	primary: map[rune]bool{},
	multi:   map[multiKey]*text.MultiFace{},
}

func (f *fallbacks) source(path string) *text.GoTextFaceSource {
	if s, ok := f.loaded[path]; ok {
		return s
	}
	f.loaded[path] = nil
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var src *text.GoTextFaceSource
	if strings.EqualFold(filepath.Ext(path), ".ttc") {
		srcs, err := text.NewGoTextFaceSourcesFromCollection(bytes.NewReader(data))
		if err == nil && len(srcs) > 0 {
			src = srcs[0]
		}
	} else {
		src, _ = text.NewGoTextFaceSource(bytes.NewReader(data))
	}
	if src != nil {
		log.Printf("font fallback: loaded %s", path)
	}
	f.loaded[path] = src
	return src
}

// sourceFor finds a fallback font with a glyph for r.
func (f *fallbacks) sourceFor(r rune) *text.GoTextFaceSource {
	if s, ok := f.pick[r]; ok {
		return s
	}
	var found *text.GoTextFaceSource
	// Already loaded fonts first, then candidates for the rune's script.
	for _, s := range f.loaded {
		if s != nil && covers(s, r) {
			found = s
			break
		}
	}
	if found == nil {
		for _, p := range fallbackCandidates(scriptOf(r)) {
			if s := f.source(p); s != nil && covers(s, r) {
				found = s
				break
			}
		}
	}
	f.pick[r] = found
	return found
}

// needsFallback is a fast check for plain Latin text.
func needsFallback(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return true
		}
	}
	return false
}

// faceFor returns the face to draw s with: the plain face for text Barlow
// covers, otherwise Barlow followed by the fallbacks s needs.
func faceFor(s string, fnt Font, size float64) text.Face {
	base := Face(fnt, size)
	if !needsFallback(s) {
		return base
	}
	fb.mu.Lock()
	defer fb.mu.Unlock()
	primary := fontSources[UI]
	var chain []*text.GoTextFaceSource
	for _, r := range s {
		if r < 0x250 {
			continue
		}
		cov, ok := fb.primary[r]
		if !ok {
			cov = covers(primary, r)
			fb.primary[r] = cov
		}
		if cov {
			continue
		}
		if src := fb.sourceFor(r); src != nil {
			dup := false
			for _, c := range chain {
				dup = dup || c == src
			}
			if !dup {
				chain = append(chain, src)
			}
		}
	}
	if len(chain) == 0 {
		return base
	}
	var key strings.Builder
	for _, c := range chain {
		for p, s := range fb.loaded {
			if s == c {
				key.WriteString(p)
				key.WriteByte('|')
			}
		}
	}
	k := multiKey{fnt, int(size * 4), key.String()}
	if m, ok := fb.multi[k]; ok {
		return m
	}
	faces := []text.Face{base}
	for _, c := range chain {
		faces = append(faces, &text.GoTextFace{Source: c, Size: size})
	}
	m, err := text.NewMultiFace(faces...)
	if err != nil {
		return base
	}
	fb.multi[k] = m
	return m
}
