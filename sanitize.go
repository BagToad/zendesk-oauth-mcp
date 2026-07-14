package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"
)

// sanitizedLead is the human-readable banner attached to _meta whenever hidden
// characters were found, so a skimming model cannot miss that the data was
// altered. The structured "sanitized" list says exactly what and where.
const sanitizedLead = "Hidden or bidirectional control characters were detected in customer-controlled values and replaced with visible {{U+XXXX}} markers to prevent bidi/zero-width text-spoofing. See \"sanitized\" for the field, codepoint, and count of each."

// sanitizedNote records one class of hidden character found in one field, so the
// model gets an explicit, structured "this was detected and replaced" signal
// out-of-band in _meta.sanitized rather than having to spot the inline markers.
type sanitizedNote struct {
	Field     string `json:"field"`     // dotted JSON path, e.g. "[0].description"
	Codepoint string `json:"codepoint"` // e.g. "U+202E"
	Name      string `json:"name"`      // e.g. "RIGHT-TO-LEFT OVERRIDE"
	Count     int    `json:"count"`     // occurrences of this codepoint in the field
}

// invisibleNames gives friendly names for the control/format characters most
// likely to be used adversarially. Anything outside this map falls back to a
// category label in runeName, so we never depend on the full Unicode database.
var invisibleNames = map[rune]string{
	0x00AD: "SOFT HYPHEN",
	0x200B: "ZERO WIDTH SPACE",
	0x200C: "ZERO WIDTH NON-JOINER",
	0x200D: "ZERO WIDTH JOINER",
	0x200E: "LEFT-TO-RIGHT MARK",
	0x200F: "RIGHT-TO-LEFT MARK",
	0x202A: "LEFT-TO-RIGHT EMBEDDING",
	0x202B: "RIGHT-TO-LEFT EMBEDDING",
	0x202C: "POP DIRECTIONAL FORMATTING",
	0x202D: "LEFT-TO-RIGHT OVERRIDE",
	0x202E: "RIGHT-TO-LEFT OVERRIDE",
	0x2060: "WORD JOINER",
	0x2066: "LEFT-TO-RIGHT ISOLATE",
	0x2067: "RIGHT-TO-LEFT ISOLATE",
	0x2068: "FIRST STRONG ISOLATE",
	0x2069: "POP DIRECTIONAL ISOLATE",
	0xFEFF: "ZERO WIDTH NO-BREAK SPACE (BOM)",
	0x3164: "HANGUL FILLER",
	0x2800: "BRAILLE PATTERN BLANK",
}

// runeName returns a human-readable name for a dangerous codepoint: the curated
// name when known, otherwise a category label.
func runeName(r rune) string {
	if n, ok := invisibleNames[r]; ok {
		return n
	}
	switch {
	case r <= 0x1F:
		return "C0 control"
	case r >= 0x80 && r <= 0x9F:
		return "C1 control"
	case r == 0x7F:
		return "DEL"
	case unicode.Is(unicode.Cf, r):
		return "format character"
	default:
		return "control character"
	}
}

// isDangerous reports whether r must not reach the model verbatim. It targets
// the Unicode Control (Cc) and Format (Cf) categories, which together cover
// C0/C1 controls, DEL, all bidi controls, and every zero-width / invisible
// formatting character. Ordinary whitespace (tab, newline, carriage return) is
// preserved — it is not an invisible spoofing vector.
//
// U+3164 HANGUL FILLER and U+2800 BRAILLE PATTERN BLANK are added explicitly:
// they render as blank but fall outside Cc/Cf (categories Lo/So), so the
// category test alone would let them through. They match the rendering &
// ingestion guidelines' "characters that aren't rendered normally".
func isDangerous(r rune) bool {
	switch r {
	case '\t', '\n', '\r':
		return false
	case 0x3164, 0x2800:
		return true
	}
	return unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cf, r)
}

// sanitize round-trips v through a generic JSON tree and replaces hidden
// characters in every reachable string with a visible {{U+XXXX}} marker, returning
// the cleaned tree plus one finding per (field, codepoint) for _meta.sanitized.
// Walking the decoded tree — instead of reflecting over the typed value — covers
// every string that reaches JSON (struct fields, slice elements, map keys and
// values, and any-typed custom fields) with no shape it can skip. UseNumber
// keeps numbers that were parsed as integers exact, so this round trip does not
// round large Zendesk IDs through float64. Object key order is not preserved:
// re-marshaling sorts keys.
//
// Findings are sorted by (field, codepoint) before returning, and walk visits
// object keys in sorted order, so identical input always yields identical
// _meta.sanitized output and identical collision handling — Go's map iteration
// order is otherwise randomized.
//
// sanitize fails closed: if v cannot be marshaled or the marshaled bytes cannot
// be decoded back into a generic tree, it returns an error rather than the raw,
// unsanitized value. The caller must surface that error instead of emitting v,
// so hidden/bidi characters can never bypass sanitization on an encoding edge.
func sanitize(v any) (any, []sanitizedNote, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, nil, fmt.Errorf("marshaling value for sanitization: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var tree any
	if err := dec.Decode(&tree); err != nil {
		return nil, nil, fmt.Errorf("decoding value for sanitization: %w", err)
	}
	var notes []sanitizedNote
	clean := walk(tree, "", &notes)
	sort.Slice(notes, func(i, j int) bool {
		if notes[i].Field != notes[j].Field {
			return notes[i].Field < notes[j].Field
		}
		return notes[i].Codepoint < notes[j].Codepoint
	})
	return clean, notes, nil
}

// walk recurses through the generic JSON tree, sanitizing each string leaf —
// including map keys, which are customer-controlled for object-shaped custom
// field values — and recording its JSON path. Non-string scalars carry no text
// and pass through. Object keys are rebuilt into a fresh map so a dangerous key
// cannot survive. Keys are visited in sorted order so the output is
// deterministic. If two source keys scrub to the same string — only reachable
// if a customer types the {{U+XXXX}} marker text verbatim, since the marker is
// otherwise unlike any real ticket text — the collision is disambiguated with a
// {{DUP<n>}} suffix rather than dropping a value, so no data is lost.
func walk(v any, path string, notes *[]sanitizedNote) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			cleanKey, keyFinds := scrub(k)
			outKey := cleanKey
			for i := 2; ; i++ {
				if _, taken := out[outKey]; !taken {
					break
				}
				outKey = fmt.Sprintf("%s{{DUP%d}}", cleanKey, i)
			}
			keyPath := joinPath(path, outKey)
			addNotes(notes, keyPath+" (key)", keyFinds)
			out[outKey] = walk(x[k], keyPath, notes)
		}
		return out
	case []any:
		for i, val := range x {
			x[i] = walk(val, fmt.Sprintf("%s[%d]", path, i), notes)
		}
		return x
	case string:
		cleaned, finds := scrub(x)
		addNotes(notes, path, finds)
		return cleaned
	default:
		return v
	}
}

// runeFinding is a per-codepoint tally from scrub, before a field path is
// attached. Keeping scrub path-free lets the caller attach the *cleaned* path,
// so a dangerous character in a map key never leaks into sanitizedNote.Field.
type runeFinding struct {
	codepoint string
	name      string
	count     int
}

// scrub replaces every dangerous character in s with a visible "{{U+XXXX}}" marker
// and returns the cleaned string plus a per-codepoint tally (sorted by codepoint
// for deterministic output). A clean string is returned unchanged with no
// findings, so the common case only pays for the initial scan.
func scrub(s string) (string, []runeFinding) {
	dangerous := false
	for _, r := range s {
		if isDangerous(r) {
			dangerous = true
			break
		}
	}
	if !dangerous {
		return s, nil
	}

	var b strings.Builder
	b.Grow(len(s))
	counts := map[rune]int{}
	for _, r := range s {
		if isDangerous(r) {
			counts[r]++
			fmt.Fprintf(&b, "{{U+%04X}}", r)
			continue
		}
		b.WriteRune(r)
	}

	runes := make([]rune, 0, len(counts))
	for r := range counts {
		runes = append(runes, r)
	}
	sort.Slice(runes, func(i, j int) bool { return runes[i] < runes[j] })
	finds := make([]runeFinding, 0, len(runes))
	for _, r := range runes {
		finds = append(finds, runeFinding{
			codepoint: fmt.Sprintf("U+%04X", r),
			name:      runeName(r),
			count:     counts[r],
		})
	}
	return b.String(), finds
}

// addNotes attaches a field path to each finding and appends it to notes.
func addNotes(notes *[]sanitizedNote, field string, finds []runeFinding) {
	for _, f := range finds {
		*notes = append(*notes, sanitizedNote{
			Field:     field,
			Codepoint: f.codepoint,
			Name:      f.name,
			Count:     f.count,
		})
	}
}

// joinPath appends a map key to a base path for _meta.sanitized reporting.
// Simple keys use dot notation (base.name). Keys that are empty or contain path
// metacharacters (. [ ] " \) are bracket-quoted (base["na.me"]) so a key literally
// named "a.b" is never confused with nested keys a -> b, and every segment stays
// unambiguous and round-trippable. Array indices are appended as [N] by the
// caller, which is already unambiguous.
func joinPath(base, name string) string {
	if isSimpleKey(name) {
		if base == "" {
			return name
		}
		return base + "." + name
	}
	return base + "[\"" + quoteKey(name) + "\"]"
}

// isSimpleKey reports whether name can be rendered with plain dot notation: it
// must be non-empty and free of the metacharacters that give a path its structure.
func isSimpleKey(name string) bool {
	return name != "" && !strings.ContainsAny(name, ".[]\"\\")
}

// quoteKey escapes backslashes and double quotes so a bracket-quoted segment is
// unambiguous and reversible.
func quoteKey(name string) string {
	r := strings.NewReplacer("\\", "\\\\", "\"", "\\\"")
	return r.Replace(name)
}
