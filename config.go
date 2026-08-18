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
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	WinTemp       int
	UserTemp      int
	Prefetch      int
	ErrorReports  int
	DeliveryOpt   int
	WinUpdate     int
	LogFiles      int
	InstallerTemp int
	DnsFlush      int
	RamOptimize   int
	RecycleBin    int
	DeepCleanup   int // AGGRESSIVE — never persisted, always starts OFF

	// --- RAM Optimization sub-options (only used when RamOptimize == 1) ---
	// The first two are the original, safe behaviour and default ON.
	// The last two free noticeably more memory but evict live/warm pages,
	// so they default OFF and are opt-in.
	RamFlushModified   int // write dirty pages to disk so they become freeable
	RamPurgeStandby    int // free the standby (cached) page list
	RamFileCache       int // flush the kernel/system file cache working set
	RamTrimWorkingSets int // page out every process's working set (aggressive)

	// Language is the optional UI language override ("en", "es", ...). Empty
	// means follow the Windows display language.
	//
	// It is read here purely so SaveConfig can write it back: the value that
	// actually selects the language is read much earlier, by InitLanguage,
	// before elevation. Without round-tripping it, saving from the settings
	// screen would silently delete the user's LANGUAGE= line.
	Language string
}

func DefaultConfig() *Config {
	return &Config{
		WinTemp:       1,
		UserTemp:      1,
		Prefetch:      1,
		ErrorReports:  1,
		DeliveryOpt:   1,
		WinUpdate:     1,
		LogFiles:      1,
		InstallerTemp: 1,
		DnsFlush:      1,
		RamOptimize:   1,
		RecycleBin:    0, // OFF by default: destructive/irreversible, so opt-in only

		RamFlushModified:   1,
		RamPurgeStandby:    1,
		RamFileCache:       0, // opt-in: drops kernel file cache
		RamTrimWorkingSets: 0, // opt-in: aggressive, causes hard faults afterwards
	}
}

// ConfigFilePath returns the path to cleaner_config.dat alongside the running
// executable, falling back to the working directory if the executable path
// cannot be determined.
//
// This is resolved at startup rather than inside LoadConfig because the
// language override is read from the same file before elevation, while
// LoadConfig only runs after it.
func ConfigFilePath() string {
	exePath, err := os.Executable()
	dir := "."
	if err == nil {
		dir = filepath.Dir(exePath)
	}
	return filepath.Join(dir, "cleaner_config.dat")
}

func LoadConfig(filePath string) (*Config, error) {
	cfg := DefaultConfig()

	file, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			errSave := SaveConfig(filePath, cfg)
			if errSave == nil {
				fmt.Printf("\n  \x1b[97m[NOTE]\x1b[0m %s\n", Tf("common.config_made", filePath))
			}
			return cfg, nil
		}
		return cfg, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])

		// LANGUAGE is the one non-numeric setting, so it is handled before
		// the Atoi below - which would otherwise reject it and skip the line.
		if strings.EqualFold(key, "LANGUAGE") {
			cfg.Language = strings.TrimSpace(parts[1])
			continue
		}

		val, parseErr := strconv.Atoi(strings.TrimSpace(parts[1]))
		if parseErr != nil {
			continue
		}

		switch key {
		case "WIN_TEMP":
			cfg.WinTemp = val
		case "USER_TEMP":
			cfg.UserTemp = val
		case "PREFETCH":
			cfg.Prefetch = val
		case "ERROR_REPORTS":
			cfg.ErrorReports = val
		case "DELIVERY_OPT":
			cfg.DeliveryOpt = val
		case "WIN_UPDATE":
			cfg.WinUpdate = val
		case "LOG_FILES":
			cfg.LogFiles = val
		case "INSTALLER_TEMP":
			cfg.InstallerTemp = val
		case "DNS_FLUSH":
			cfg.DnsFlush = val
		case "RAM_OPTIMIZE":
			cfg.RamOptimize = val
		case "RECYCLE_BIN":
			cfg.RecycleBin = val
		case "RAM_FLUSH_MODIFIED":
			cfg.RamFlushModified = val
		case "RAM_PURGE_STANDBY":
			cfg.RamPurgeStandby = val
		case "RAM_FILE_CACHE":
			cfg.RamFileCache = val
		case "RAM_TRIM_WORKING_SETS":
			cfg.RamTrimWorkingSets = val
		}
	}

	return cfg, nil
}

func SaveConfig(filePath string, cfg *Config) error {
	content := fmt.Sprintf(
		"WIN_TEMP=%d\nUSER_TEMP=%d\nPREFETCH=%d\nERROR_REPORTS=%d\nDELIVERY_OPT=%d\nWIN_UPDATE=%d\nLOG_FILES=%d\nINSTALLER_TEMP=%d\nDNS_FLUSH=%d\nRAM_OPTIMIZE=%d\nRECYCLE_BIN=%d\nRAM_FLUSH_MODIFIED=%d\nRAM_PURGE_STANDBY=%d\nRAM_FILE_CACHE=%d\nRAM_TRIM_WORKING_SETS=%d\n",
		cfg.WinTemp,
		cfg.UserTemp,
		cfg.Prefetch,
		cfg.ErrorReports,
		cfg.DeliveryOpt,
		cfg.WinUpdate,
		cfg.LogFiles,
		cfg.InstallerTemp,
		cfg.DnsFlush,
		cfg.RamOptimize,
		cfg.RecycleBin,
		cfg.RamFlushModified,
		cfg.RamPurgeStandby,
		cfg.RamFileCache,
		cfg.RamTrimWorkingSets,
	)

	// Preserve an explicit language override across saves. It is only written
	// when the user actually set one, so a default config file stays free of
	// a setting most people never touch.
	if lang := strings.TrimSpace(cfg.Language); lang != "" {
		content += fmt.Sprintf("LANGUAGE=%s\n", lang)
	}

	return os.WriteFile(filePath, []byte(content), 0644)
}

func (c *Config) GetVal(index int) int {
	switch index {
	case 1:
		return c.WinTemp
	case 2:
		return c.UserTemp
	case 3:
		return c.Prefetch
	case 4:
		return c.ErrorReports
	case 5:
		return c.DeliveryOpt
	case 6:
		return c.WinUpdate
	case 7:
		return c.LogFiles
	case 8:
		return c.InstallerTemp
	case 9:
		return c.DnsFlush
	case 10:
		return c.RamOptimize
	case 11:
		return c.RecycleBin
	// 101-104: RAM Optimization sub-options. Kept outside the 1..11 range so
	// the main-menu loop over the top-level steps is unaffected.
	case 101:
		return c.RamFlushModified
	case 102:
		return c.RamPurgeStandby
	case 103:
		return c.RamFileCache
	case 104:
		return c.RamTrimWorkingSets
	default:
		return 0
	}
}

func (c *Config) SetVal(index int, val int) {
	switch index {
	case 1:
		c.WinTemp = val
	case 2:
		c.UserTemp = val
	case 3:
		c.Prefetch = val
	case 4:
		c.ErrorReports = val
	case 5:
		c.DeliveryOpt = val
	case 6:
		c.WinUpdate = val
	case 7:
		c.LogFiles = val
	case 8:
		c.InstallerTemp = val
	case 9:
		c.DnsFlush = val
	case 10:
		c.RamOptimize = val
	case 11:
		c.RecycleBin = val
	case 101:
		c.RamFlushModified = val
	case 102:
		c.RamPurgeStandby = val
	case 103:
		c.RamFileCache = val
	case 104:
		c.RamTrimWorkingSets = val
	}
}
