//go:build windows

// Tray localization.
//
// The texts live in separate JSON files under locales/ and are baked into the
// exe with go:embed - nothing shows up on disk next to the program, and a new
// language can be added with a single file, without touching the code. English
// is the source of truth: the set of keys is taken from it, and any key missing
// from a translation is silently replaced by the English one, so an incomplete
// translation does not break the interface - it only partly shows through in
// English.
package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

//go:embed locales/*.json
var localeFS embed.FS

const fallbackLang = "en"

var (
	locales   = map[string]map[string]string{}
	langCodes []string // codes of all loaded languages, in menu display order
	curLang   = fallbackLang
)

func init() {
	entries, err := localeFS.ReadDir("locales")
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		b, err := localeFS.ReadFile("locales/" + name)
		if err != nil {
			continue
		}
		m := map[string]string{}
		if json.Unmarshal(b, &m) != nil {
			continue // a broken translation file must not take the program down
		}
		// service keys (notes for translators) never reach the interface
		for k := range m {
			if strings.HasPrefix(k, "_") {
				delete(m, k)
			}
		}
		code := strings.TrimSuffix(name, ".json")
		locales[code] = m
		langCodes = append(langCodes, code)
	}

	// menu order is by the language's own name, so the list does not jump from
	// build to build (ReadDir order is not guaranteed)
	sort.Slice(langCodes, func(i, j int) bool {
		return langName(langCodes[i]) < langName(langCodes[j])
	})
}

// langName - the language's own name for the menu item ("Deutsch", not
// "German").
func langName(code string) string {
	if m, ok := locales[code]; ok {
		if n, ok := m["lang.name"]; ok && n != "" {
			return n
		}
	}
	return code
}

// setLang picks the language. An empty string means "same as Windows".
func setLang(pref string) {
	if pref != "" {
		if _, ok := locales[pref]; ok {
			curLang = pref
			return
		}
	}
	curLang = detectLang()
}

// detectLang derives the language from the Windows interface language.
//
// We deliberately take the UI language (the one the user actually sees the
// system in) and not the regional settings: someone running Windows in Russian
// with US date formats expects a Russian interface, not an English one.
func detectLang() string {
	r, _, _ := pGetUserDefaultUILanguage.Call()
	// LANGID: low 10 bits are the primary language, high 6 the variant (dialect)
	primary := uint16(r) & 0x3ff

	// The variant only counts where it changes which file we pick: our
	// Portuguese is the Brazilian one, our Chinese is simplified.
	code, ok := map[uint16]string{
		0x09: "en",
		0x19: "ru",
		0x22: "uk",
		0x07: "de",
		0x0c: "fr",
		0x0a: "es",
		0x10: "it",
		0x15: "pl",
		0x16: "pt",
		0x1f: "tr",
		0x11: "ja",
		0x04: "zh",
	}[primary]
	if !ok {
		return fallbackLang
	}
	if _, have := locales[code]; !have {
		return fallbackLang
	}
	return code
}

// T returns the string for a key in the current language.
//
// If the key is missing from the translation we take the English one, and if it
// is missing there too, the key itself. That way an unfinished translation
// gives an English label rather than a blank spot in the menu.
func T(key string, args ...any) string {
	s, ok := locales[curLang][key]
	if !ok || s == "" {
		s, ok = locales[fallbackLang][key]
	}
	if !ok || s == "" {
		return key
	}
	if len(args) == 0 {
		return s
	}
	// A translation may have lost the placeholder. Without this check fmt would
	// append "%!(EXTRA int=2247)" to the interface text - better to show the
	// string without the number.
	if !strings.Contains(s, "%") {
		return s
	}
	return fmt.Sprintf(s, args...)
}
