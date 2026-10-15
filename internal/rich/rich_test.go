package rich

import (
	"image/color"
	"testing"
)

func TestParse(t *testing.T) {
	sp := Parse("<color=#33ccff><b>gem</b></color>")
	if len(sp) != 1 || sp[0].Text != "gem" || !sp[0].Bold || !sp[0].HasColor || sp[0].Color != (color.RGBA{0x33, 0xCC, 0xFF, 0xFF}) {
		t.Fatalf("charter tag: %+v", sp)
	}
	sp = Parse("<color=#B00B69>Ca</color><color=#DDDD00>mL</color>ike")
	if len(sp) != 3 || sp[0].Text != "Ca" || sp[1].Color.R != 0xDD || sp[2].HasColor {
		t.Fatalf("multi color: %+v", sp)
	}
	// nested colors restore the outer one
	sp = Parse("<color=red>a<color=blue>b</color>c</color>d")
	if len(sp) != 4 || sp[2].Color != namedColors["red"] || sp[3].HasColor {
		t.Fatalf("nesting: %+v", sp)
	}
	if got := Plain("Hot <i>Mulligan</i><br>live <cspace=2>x</cspace><sprite=3>"); got != "Hot Mulligan live x" {
		t.Errorf("plain = %q", got)
	}
	// not tags: kept as text
	if got := Plain("a < b > c <3"); got != "a < b > c <3" {
		t.Errorf("plain = %q", got)
	}
	if got := Plain("Rock <and> Roll"); got != "Rock <and> Roll" {
		t.Errorf("unknown tag word should stay, got %q", got)
	}
	if c, ok := ParseColor("#f80"); !ok || c != (color.RGBA{0xFF, 0x88, 0x00, 0xFF}) {
		t.Errorf("#rgb: %v %v", c, ok)
	}
	if c, ok := ParseColor("#11223380"); !ok || c.A != 0x80 {
		t.Errorf("#rrggbbaa: %v %v", c, ok)
	}
}
