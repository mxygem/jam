package app

import (
	"fmt"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/mxygem/jam/internal/action"
	"github.com/mxygem/jam/internal/config"
	"github.com/mxygem/jam/internal/render"
)

// filterEditor edits the song list filter in place; the list updates live.
type filterEditor struct {
	f       *config.Filter
	list    List
	parts   bool // editing the part checkboxes
	rule    int  // editing this field rule (-1 none)
	typing  bool
	input   TextInput
	orig    string
	changed bool
}

func newFilterEditor(a *App) *filterEditor {
	e := &filterEditor{f: &a.Settings.Browse.Filter, rule: -1}
	if e.f.InstMode == "" {
		e.f.InstMode = "all"
	}
	e.build()
	return e
}

var instModes = []sortKey{{"all", "Has all of them"}, {"any", "Has any of them"}, {"only", "Has only these"}}

func modeLabel(id string) string {
	for _, m := range instModes {
		if m.id == id {
			return m.label
		}
	}
	return instModes[0].label
}

func (e *filterEditor) touch() { e.changed = true }

func (e *filterEditor) build() {
	f := e.f
	sel := e.list.Sel
	var items []Item
	switch {
	case e.parts:
		for _, p := range filterParts {
			p := p
			items = append(items, Item{Label: partLabel(p), Value: func() string {
				if containsStr(f.Instruments, p) {
					return "[x]"
				}
				return "[ ]"
			}, Activate: func() {
				if containsStr(f.Instruments, p) {
					f.Instruments = removeStr(f.Instruments, p)
				} else {
					f.Instruments = append(f.Instruments, p)
				}
				f.Enabled = true
				e.touch()
			}})
		}
		items = append(items, Item{Label: "Done", Activate: func() { e.parts = false; e.list.Sel = 1; e.build() }})
	case e.rule >= 0:
		r := &f.Fields[e.rule]
		items = []Item{
			{Label: "Field", Value: func() string { return fieldLabel(r.Field) }, Adjust: func(d int) {
				idx := 0
				for i, k := range filterFields {
					if k.id == r.Field {
						idx = i
					}
				}
				r.Field = filterFields[(idx+d+len(filterFields))%len(filterFields)].id
				e.touch()
			}},
			{Label: "Contains", Value: func() string { return quote(r.Text) }, Activate: func() { e.startTyping(r.Text) }},
			{Label: "Remove this rule", Activate: func() {
				f.Fields = append(f.Fields[:e.rule], f.Fields[e.rule+1:]...)
				e.rule = -1
				e.list.Sel = 0
				e.touch()
				e.build()
			}},
			{Label: "Done", Activate: func() { e.rule = -1; e.build() }},
		}
	default:
		items = []Item{
			{Label: "Filtering", Value: func() string { return onOff(f.Enabled) }, Adjust: func(int) { f.Enabled = !f.Enabled; e.touch() }},
			{Label: "Parts", Value: func() string {
				if len(f.Instruments) == 0 {
					return "Any (none chosen)"
				}
				names := make([]string, len(f.Instruments))
				for i, p := range f.Instruments {
					names[i] = partLabel(p)
				}
				return truncateW(strings.Join(names, ", "), render.UI, 32, 420)
			}, Activate: func() { e.parts = true; e.list.Sel = 0; e.build() }},
			{Label: "Match", Value: func() string { return modeLabel(f.InstMode) }, Adjust: func(d int) {
				idx := 0
				for i, m := range instModes {
					if m.id == f.InstMode {
						idx = i
					}
				}
				f.InstMode = instModes[(idx+d+len(instModes))%len(instModes)].id
				e.touch()
			}, Hint: "All: has every part picked. Any: has at least one. Only: has the parts picked and nothing else."},
		}
		for i := range f.Fields {
			i := i
			items = append(items, Item{Label: fmt.Sprintf("%s contains", fieldLabel(f.Fields[i].Field)),
				Value:    func() string { return quote(f.Fields[i].Text) },
				Activate: func() { e.rule = i; e.list.Sel = 0; e.build() }})
		}
		items = append(items,
			Item{Label: "Add a field rule...", Hint: "Pick a field (title, artist, charter...) and the text it must contain", Activate: func() {
				f.Fields = append(f.Fields, config.FieldFilter{Field: "title"})
				f.Enabled = true
				e.rule = len(f.Fields) - 1
				e.build()
				e.list.Sel = 0
			}},
			Item{Label: "Clear all rules", Activate: func() {
				f.Instruments, f.Fields = nil, nil
				e.touch()
				e.build()
			}},
			Item{Label: "Done"},
		)
	}
	e.list = List{Items: items, Visible: 9, Sel: min(sel, len(items)-1)}
}

func quote(s string) string {
	if s == "" {
		return "(anything)"
	}
	return "\"" + truncateW(s, render.UI, 32, 380) + "\""
}

func (e *filterEditor) startTyping(v string) {
	e.typing = true
	e.orig = v
	e.input = TextInput{Value: v, Placeholder: "type text to look for"}
}

// update returns true when the editor should close.
func (e *filterEditor) update(a *App) bool {
	if e.typing {
		done, cancel := e.input.Update(a)
		if e.rule >= 0 && e.rule < len(e.f.Fields) && e.f.Fields[e.rule].Text != e.input.Value && !cancel {
			e.f.Fields[e.rule].Text = e.input.Value // live
			e.touch()
		}
		if cancel && e.rule >= 0 && e.rule < len(e.f.Fields) {
			e.f.Fields[e.rule].Text = e.orig
			e.touch()
		}
		if done || cancel {
			e.typing = false
		}
		return false
	}
	if e.list.Update(a) {
		if !e.parts && e.rule < 0 && e.list.Items[e.list.Sel].Label == "Done" {
			return true
		}
		return false
	}
	if a.Pressed(action.MenuBack) || a.Mouse.Right {
		switch {
		case e.parts:
			e.parts = false
			e.build()
		case e.rule >= 0:
			e.rule = -1
			e.build()
		default:
			return true
		}
	}
	return false
}

func (e *filterEditor) draw(dst *ebiten.Image, a *App) {
	render.FillRect(dst, 0, 0, render.ScreenW, render.ScreenH, render.RGBA(0, 0, 0, 0xB8))
	Panel(dst, 420, 150, 1080, 820)
	title := "Filter songs"
	switch {
	case e.parts:
		title = "Filter by parts"
	case e.rule >= 0:
		title = "Field rule"
	}
	render.Text(dst, title, render.Display, 52, 460, 170, th.Text, render.Left)
	e.list.Draw(dst, 480, 270, 960, 68)
	hint := e.list.Hint()
	if e.parts {
		hint = "Vocals means the chart has lyrics. Matching uses: " + strings.ToLower(modeLabel(e.f.InstMode)) + "."
	}
	render.Text(dst, hint, render.UI, 24, 460, 900, th.TextDim, render.Left)
	if e.typing {
		render.FillRect(dst, 0, 0, render.ScreenW, render.ScreenH, render.RGBA(0, 0, 0, 0x60))
		Panel(dst, 520, 420, 880, 220)
		render.Text(dst, fieldLabel(e.f.Fields[e.rule].Field)+" contains", render.UIBold, 32, 560, 440, th.Text, render.Left)
		e.input.Draw(dst, 560, 500, 800)
		render.Text(dst, "Enter to keep   Esc to cancel   Ctrl+Backspace clears", render.Cond, 22, 560, 590, th.TextDim, render.Left)
	}
}

func containsStr(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func removeStr(xs []string, s string) []string {
	var out []string
	for _, x := range xs {
		if x != s {
			out = append(out, x)
		}
	}
	return out
}
