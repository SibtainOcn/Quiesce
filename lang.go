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

import (
	"fmt"

	ki18n "github.com/itskreisler/i18n-go"
	"golang.org/x/sys/windows"

	"quiesce/locales"
)

var (
	modkernel32Lang              = windows.NewLazySystemDLL("kernel32.dll")
	procGetUserDefaultUILanguage = modkernel32Lang.NewProc("GetUserDefaultUILanguage")
)

// loc is the active localizer. It is initialized with the detected Windows
// display language before any output is printed, and re-initialized after the
// config is loaded if it carries a LANGUAGE= override. go-i18n falls back to
// English for any key a locale does not define.
var loc *ki18n.Localizer

// initLocalizer builds the localizer for the given language code. An empty
// language means "detect from the Windows display language". Any code we do
// not ship a locale for falls back to English.
func initLocalizer(lang string) {
	if lang == "" {
		lang = detectSystemLang()
	}
	if lang != "es" {
		lang = "en"
	}

	var err error
	if lang == "en" {
		loc, err = ki18n.New(locales.FS, "en")
	} else {
		loc, err = ki18n.New(locales.FS, lang, "en")
	}
	if err != nil {
		panic(fmt.Sprintf("i18n init failed: %v", err))
	}
}

// T returns the localized message for key.
func T(key ki18n.Key) string {
	if loc == nil {
		return string(key)
	}
	return loc.T(key)
}

// TD returns the localized message for key with template data (e.g. {{.Count}}).
func TD(key ki18n.Key, data map[string]any) string {
	if loc == nil {
		return string(key)
	}
	return loc.TD(key, data)
}

// detectSystemLang maps the Windows display language to one of the shipped
// locales. The primary language is the low 10 bits of the LANGID returned by
// GetUserDefaultUILanguage; anything other than Spanish falls back to English.
func detectSystemLang() string {
	r1, _, _ := procGetUserDefaultUILanguage.Call()
	switch uint32(r1) & 0x3FF {
	case 10: // LANG_SPANISH
		return "es"
	default:
		return "en"
	}
}
