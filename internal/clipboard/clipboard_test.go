package clipboard

import "testing"

func TestLine(t *testing.T) {
	for in, want := range map[string]string{
		"E:\\charts\\complete":            "E:\\charts\\complete",
		"  \"E:\\my songs\"  \r\n":        "\"E:\\my songs\"",
		"\r\n\r\nE:\\a\r\nE:\\b":          "E:\\a",
		"/home/gem/songs\twith tab\x00\n": "/home/gem/songs with tab",
		"":                                "",
		"\n \n":                           "",
	} {
		if got := Line(in); got != want {
			t.Errorf("Line(%q) = %q, want %q", in, got, want)
		}
	}
}
