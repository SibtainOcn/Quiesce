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
	"strings"
	"testing"

	"quiesce/locales"
)

func TestInitLocalizerEnglish(t *testing.T) {
	initLocalizer("en")
	if got := T(locales.MenuThisWillClean); got != "This will clean:" {
		t.Errorf("menu.this_will_clean (en) = %q, want %q", got, "This will clean:")
	}
	if got := T(locales.StepWinTemp); got != "Windows Temp folder" {
		t.Errorf("step.win_temp (en) = %q, want %q", got, "Windows Temp folder")
	}
}

func TestInitLocalizerSpanish(t *testing.T) {
	initLocalizer("es")
	if got := T(locales.MenuThisWillClean); got != "Esto limpiará:" {
		t.Errorf("menu.this_will_clean (es) = %q, want %q", got, "Esto limpiará:")
	}
	if got := T(locales.StepWinTemp); got != "Carpeta Temp de Windows" {
		t.Errorf("step.win_temp (es) = %q, want %q", got, "Carpeta Temp de Windows")
	}
}

func TestTemplateRendering(t *testing.T) {
	initLocalizer("es")
	got := TD(locales.SummaryItems, map[string]any{"Count": 42})
	if !strings.Contains(got, "42") || !strings.Contains(got, "elementos") {
		t.Errorf("summary.items (es) rendered = %q, want it to contain 42 and elementos", got)
	}
}

func TestUnknownLanguageFallsBackToEnglish(t *testing.T) {
	initLocalizer("fr") // not shipped -> must fall back to English
	if got := T(locales.StepWinTemp); got != "Windows Temp folder" {
		t.Errorf("step.win_temp (fr fallback) = %q, want English %q", got, "Windows Temp folder")
	}
}
