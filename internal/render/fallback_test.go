package render

import (
	"testing"

	"github.com/hajimehoshi/ebiten/v2/text/v2"
)

func TestFallbackFace(t *testing.T) {
	if _, ok := faceFor("Crow", UI, 30).(*text.GoTextFace); !ok {
		t.Fatal("plain Latin text should use the plain face")
	}
	if _, ok := faceFor("García", UI, 30).(*text.GoTextFace); !ok {
		t.Fatal("Latin-1 text is covered by Barlow")
	}
	f := faceFor("김진산", UI, 30)
	if _, ok := f.(*text.MultiFace); !ok {
		t.Skip("no Hangul font installed here")
	}
	w, _ := Measure("김진산", UI, 30)
	if w < 60 {
		t.Fatalf("Hangul measured %.0f px wide, glyphs missing?", w)
	}
	if faceFor("김진산", UI, 30) != f {
		t.Fatal("fallback faces should be cached")
	}
	if _, ok := faceFor("ヒゲダンディズム 髭", CondBold, 24).(*text.MultiFace); !ok {
		t.Fatal("kana and kanji should get a fallback too")
	}
}
