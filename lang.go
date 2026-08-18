// Quiesce - Windows system cleaner and RAM optimizer.
// Copyright (C) 2026 SibtainOcn <https://github.com/SibtainOcn/Quiesce>
//
// This program is free software: you can redistribute it and/or modify it
// under the terms of the GNU General Public License as published by the Free
// Software Foundation, either version 3 of the License, or (at your option)
// any later version.
//
// This program is distributed in the hope that it will be useful, but WITHOUT
// ANY WARRANTY; without even the implied warranty of MERCHANTABILITY or
// FITNESS FOR A PARTICULAR PURPOSE. See the GNU General Public License for
// more details.
//
// You should have received a copy of the GNU General Public License along
// with this program. If not, see <https://www.gnu.org/licenses/>.

package main

// Localization for the Quiesce console UI, built on nicksnyder/go-i18n (MIT).
//
// Translations live in locales/active.<code>.toml and are embedded into the
// binary at build time, so qc.exe stays a single portable file with no
// sidecar files to ship or lose.
//
// Adding a language requires no code change at all: drop
// locales/active.<code>.toml next to the others and rebuild. The whole
// directory is embedded and parsed, and the language is matched against the
// Windows display language automatically.
//
// Every lookup falls back to English, and finally to the key itself, so a
// missing or half-finished translation degrades per-string instead of
// printing blank lines on a screen the user is about to run as Administrator.

import (
	"bufio"
	"embed"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/sys/windows"
	"golang.org/x/text/language"
)

//go:embed locales/*.toml
var localeFS embed.FS

var (
	modkernel32Lang              = windows.NewLazySystemDLL("kernel32.dll")
	procGetUserDefaultUILanguage = modkernel32Lang.NewProc("GetUserDefaultUILanguage")
)

var (
	bundle     *i18n.Bundle
	localizer  *i18n.Localizer
	activeCode = "en"
)

// init builds the message bundle from the embedded locale files. It runs
// before main(), so T() is safe to call from anywhere - including the very
// first line printed by --version or the elevation prompt.
//
// Parse failures are skipped rather than fatal: one malformed community
// translation file must not stop the app from starting in English.
func init() {
	bundle = i18n.NewBundle(language.English)
	bundle.RegisterUnmarshalFunc("toml", toml.Unmarshal)

	entries, err := localeFS.ReadDir("locales")
	if err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
				continue
			}
			data, readErr := localeFS.ReadFile("locales/" + e.Name())
			if readErr != nil {
				continue
			}
			// The language is inferred from the filename, e.g.
			// "active.es.toml" registers Spanish.
			_, _ = bundle.ParseMessageFileBytes(data, e.Name())
		}
	}

	localizer = i18n.NewLocalizer(bundle, "en")
}

// T returns the translated string for key. go-i18n resolves the active
// language first and English second; if the key exists in neither, the key
// itself is returned so a typo is visible on screen rather than silent.
func T(key string) string {
	s, err := localizer.Localize(&i18n.LocalizeConfig{MessageID: key})
	if err != nil || s == "" {
		return key
	}
	return s
}

// Tf is T followed by Sprintf. The format verbs live inside the translated
// string so translators can reorder them to suit their language.
func Tf(key string, a ...interface{}) string {
	return fmt.Sprintf(T(key), a...)
}

// ActiveLanguage reports the language code currently in use.
func ActiveLanguage() string { return activeCode }

// languageNames holds each language's name written in that language itself.
//
// Language menus are never translated: a Spanish speaker looking at an
// English UI needs to find "Español", not "Spanish". Adding a language means
// adding one entry here alongside its locale file.
var languageNames = map[string]string{
	"en": "English",
	"es": "Español",
}

// AvailableLanguages lists every language embedded in this build, sorted so
// the picker and --version always show them in the same order.
func AvailableLanguages() []string {
	tags := bundle.LanguageTags()
	codes := make([]string, 0, len(tags))
	for _, t := range tags {
		codes = append(codes, t.String())
	}
	sort.Strings(codes)
	return codes
}

// LanguageDisplayName returns a language's own name for the picker, falling
// back to the bare code so a locale file added without a name entry still
// shows up as something selectable rather than a blank row.
func LanguageDisplayName(code string) string {
	if name, ok := languageNames[strings.ToLower(code)]; ok {
		return name
	}
	return code
}

// SetLanguage switches the active language, always keeping English as the
// fallback. An unknown code is not an error: go-i18n simply resolves
// everything through English, which is the behaviour we want for a bad
// LANGUAGE= line - the app runs, in English, instead of refusing to start.
func SetLanguage(code string) {
	code = strings.ToLower(strings.TrimSpace(code))
	if code == "" {
		code = "en"
	}
	localizer = i18n.NewLocalizer(bundle, code, "en")
	activeCode = code
}

// InitLanguage picks the UI language: the Windows display language by
// default, overridden by a LANGUAGE= line in the config file if present.
//
// This runs before anything is printed - including --version/--help and the
// elevation prompt - so it reads the override itself rather than waiting for
// LoadConfig, which only happens after elevation.
func InitLanguage(configPath string) {
	code := detectSystemLanguage()
	if override := readLanguageOverride(configPath); override != "" {
		code = override
	}
	SetLanguage(code)
}

// detectSystemLanguage maps the Windows display language to a language code.
//
// GetUserDefaultUILanguage returns a LANGID whose low 10 bits identify the
// primary language. Only the primary language is used, so es-ES and es-MX
// both resolve to "es" - the UI has no region-specific wording, and matching
// on the full LANGID would leave most Spanish installs on English.
func detectSystemLanguage() string {
	if procGetUserDefaultUILanguage.Find() != nil {
		return "en"
	}
	r1, _, _ := procGetUserDefaultUILanguage.Call()
	return primaryLangCode(uint16(r1))
}

// primaryLangCode converts a Windows LANGID to an ISO 639-1 code. Codes
// without a locale file resolve through English at lookup time, so this list
// only needs a case per language actually shipped.
func primaryLangCode(langID uint16) string {
	switch langID & 0x3FF {
	case 0x09:
		return "en"
	case 0x0A:
		return "es"
	default:
		return "en"
	}
}

// readLanguageOverride scans the config file for a LANGUAGE= line. It is
// deliberately standalone and failure-tolerant: the file may not exist yet on
// first run, and this is called before LoadConfig.
func readLanguageOverride(configPath string) string {
	if configPath == "" {
		return ""
	}
	file, err := os.Open(configPath)
	if err != nil {
		return ""
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 && strings.EqualFold(strings.TrimSpace(parts[0]), "LANGUAGE") {
			return strings.TrimSpace(parts[1])
		}
	}
	return ""
}

// --- Layout helpers ------------------------------------------------------
//
// The console UI aligns its ON/OFF column by padding labels to a fixed width.
// That padding used to be typed into the English strings as trailing spaces,
// which breaks the moment a label is translated - Spanish labels run 20-30%
// longer than their English source.
//
// So the width is measured at runtime from the strings actually in use, in
// RUNES rather than bytes: "Optimización" is 12 characters but 13 bytes in
// UTF-8, and padding by byte length would misalign every accented label.

// padRight pads s with spaces to width w (measured in runes). Labels longer
// than w are returned unchanged rather than truncated - a clipped label is
// worse than a nudged column.
func padRight(s string, w int) string {
	if n := w - len([]rune(s)); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

// maxKeyWidth returns the rune width of the longest translated value among
// the given keys, in the active language.
func maxKeyWidth(keys ...string) int {
	w := 0
	for _, k := range keys {
		if n := len([]rune(T(k))); n > w {
			w = n
		}
	}
	return w
}
