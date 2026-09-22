package skyhub

import (
	"fmt"
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// Form is an HTML form with the values a browser would submit untouched,
// plus enough metadata to emulate the hub's dataToHidden() JavaScript.
type Form struct {
	Index  int
	Name   string
	ID     string
	Action string
	Values url.Values

	fields []formField // document order
	byName map[string]*formField
}

type formField struct {
	name    string
	typ     string   // hidden|text|password|checkbox|radio|select|textarea|...
	value   string   // static value attr (checkbox/radio) or "on"
	options []string // select
}

// Forms returns every <form> on the page, in document order.
func (p *Page) Forms() ([]*Form, error) {
	doc, err := p.Doc()
	if err != nil {
		return nil, err
	}
	var forms []*Form
	walk(doc, func(n *html.Node) bool {
		if n.Data != "form" {
			return true
		}
		f := &Form{
			Index:  len(forms),
			Name:   attrOr(n, "name", ""),
			ID:     attrOr(n, "id", ""),
			Action: attrOr(n, "action", ""),
			Values: url.Values{},
			byName: map[string]*formField{},
		}
		f.collect(n)
		forms = append(forms, f)
		return false
	})
	return forms, nil
}

// Form selects a form by "name=X", "id=X", "action=X" or "index=N".
func (p *Page) Form(sel string) (*Form, error) {
	forms, err := p.Forms()
	if err != nil {
		return nil, err
	}
	k, v, ok := strings.Cut(sel, "=")
	if !ok {
		return nil, fmt.Errorf("skyhub: bad form selector %q", sel)
	}
	for _, f := range forms {
		switch k {
		case "name":
			if f.Name == v {
				return f, nil
			}
		case "id":
			if f.ID == v {
				return f, nil
			}
		case "action":
			if f.Action == v {
				return f, nil
			}
		case "index":
			if strconv.Itoa(f.Index) == v {
				return f, nil
			}
		}
	}
	return nil, &ParseError{Page: p.Path, What: "form not found: " + sel}
}

func (f *Form) add(ff formField, submitted bool, value string) {
	f.fields = append(f.fields, ff)
	if _, dup := f.byName[ff.name]; !dup {
		f.byName[ff.name] = &f.fields[len(f.fields)-1]
	}
	if submitted {
		f.Values.Add(ff.name, value)
	}
}

func (f *Form) collect(form *html.Node) {
	walk(form, func(n *html.Node) bool {
		switch n.Data {
		case "input":
			name, ok := attr(n, "name")
			if !ok || name == "" {
				return true
			}
			typ := strings.ToLower(attrOr(n, "type", "text"))
			val := attrOr(n, "value", "")
			switch typ {
			case "checkbox":
				if val == "" {
					val = "on"
				}
				_, checked := attr(n, "checked")
				f.add(formField{name: name, typ: typ, value: val}, checked, val)
			case "radio":
				_, checked := attr(n, "checked")
				f.add(formField{name: name, typ: typ, value: val}, checked, val)
			case "submit", "button", "image", "reset", "file":
				f.add(formField{name: name, typ: typ, value: val}, false, "")
			default:
				f.add(formField{name: name, typ: typ, value: val}, true, val)
			}
		case "select":
			name, ok := attr(n, "name")
			if !ok || name == "" {
				return true
			}
			ff := formField{name: name, typ: "select"}
			selected := ""
			hasSel := false
			walk(n, func(o *html.Node) bool {
				if o.Data != "option" {
					return true
				}
				v, hasV := attr(o, "value")
				if !hasV {
					v = nodeText(o)
				}
				ff.options = append(ff.options, v)
				if _, s := attr(o, "selected"); s && !hasSel {
					selected, hasSel = v, true
				}
				return false
			})
			if !hasSel && len(ff.options) > 0 {
				selected = ff.options[0]
			}
			f.add(ff, len(ff.options) > 0, selected)
			return false
		case "textarea":
			name, ok := attr(n, "name")
			if !ok || name == "" {
				return true
			}
			f.add(formField{name: name, typ: "textarea"}, true, nodeText(n))
			return false
		}
		return true
	})
}

// Has reports whether the form declares a field with this name.
func (f *Form) Has(name string) bool { _, ok := f.byName[name]; return ok }

// Get returns the first submitted value for name.
func (f *Form) Get(name string) string { return f.Values.Get(name) }

// Set replaces the submitted value for name.
func (f *Form) Set(name, value string) { f.Values.Set(name, value) }

// Del removes name from the submitted values.
func (f *Form) Del(name string) { f.Values.Del(name) }

// SetIPv4 fills the four octet fields prefix1..prefix4 (the c4_prefix mirror
// is computed by ApplyDataToHidden).
func (f *Form) SetIPv4(prefix string, ip netip.Addr) {
	if !ip.Is4() {
		for i := 1; i <= 4; i++ {
			f.Set(prefix+strconv.Itoa(i), "")
		}
		return
	}
	b := ip.As4()
	for i := 0; i < 4; i++ {
		f.Set(prefix+strconv.Itoa(i+1), strconv.Itoa(int(b[i])))
	}
}

// IPv4 reads the four octet fields prefix1..prefix4.
func (f *Form) IPv4(prefix string) (netip.Addr, bool) {
	var b [4]byte
	for i := 0; i < 4; i++ {
		n, err := strconv.Atoi(strings.TrimSpace(f.Get(prefix + strconv.Itoa(i+1))))
		if err != nil || n < 0 || n > 255 {
			return netip.Addr{}, false
		}
		b[i] = byte(n)
	}
	return netip.AddrFrom4(b), true
}

// SetCheckbox checks or unchecks a checkbox the way a browser would: the
// field is present with its value attribute when checked and absent
// otherwise. The h_<name> mirror is filled by ApplyDataToHidden.
func (f *Form) SetCheckbox(name string, on bool) {
	if !on {
		f.Del(name)
		return
	}
	val := "on"
	if ff, ok := f.byName[name]; ok && ff.value != "" {
		val = ff.value
	}
	f.Set(name, val)
}

// Options returns the option values of a select field.
func (f *Form) Options(name string) []string {
	if ff, ok := f.byName[name]; ok {
		return ff.options
	}
	return nil
}

// ApplyDataToHidden replicates the hub's dataToHidden(form) JavaScript that
// runs before every submit:
//
//   - c4_X  is set to "X1.X2.X3.X4" from the octet fields;
//   - h_X   is set from X: the selected option of a select, "enable"/"disable"
//     for a checkbox, the checked value of a radio group.
//
// It must be called after all other values are set.
func (f *Form) ApplyDataToHidden() {
	seenRadio := map[string]bool{}
	for _, ff := range f.fields {
		if strings.HasPrefix(ff.name, "c4_") {
			base := ff.name[3:]
			var parts []string
			for i := 1; i <= 4; i++ {
				parts = append(parts, strings.TrimSpace(f.Get(base+strconv.Itoa(i))))
			}
			f.Set(ff.name, strings.Join(parts, "."))
		}
		if !f.Has("h_" + ff.name) {
			continue
		}
		switch ff.typ {
		case "select":
			f.Set("h_"+ff.name, f.Get(ff.name))
		case "checkbox":
			if _, on := f.Values[ff.name]; on {
				f.Set("h_"+ff.name, "enable")
			} else {
				f.Set("h_"+ff.name, "disable")
			}
		case "radio":
			if seenRadio[ff.name] {
				continue
			}
			seenRadio[ff.name] = true
			f.Set("h_"+ff.name, f.Get(ff.name))
		}
	}
}

// Encode returns the urlencoded body.
func (f *Form) Encode() string { return f.Values.Encode() }
