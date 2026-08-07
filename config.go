package main

import (
	"bufio"
	"fmt"
	"os"
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
	}
}

func LoadConfig(filePath string) (*Config, error) {
	cfg := DefaultConfig()

	file, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			errSave := SaveConfig(filePath, cfg)
			if errSave == nil {
				fmt.Println("\n  \x1b[97m[NOTE]\x1b[0m Config created: " + filePath)
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
		}
	}

	return cfg, nil
}

func SaveConfig(filePath string, cfg *Config) error {
	content := fmt.Sprintf(
		"WIN_TEMP=%d\nUSER_TEMP=%d\nPREFETCH=%d\nERROR_REPORTS=%d\nDELIVERY_OPT=%d\nWIN_UPDATE=%d\nLOG_FILES=%d\nINSTALLER_TEMP=%d\nDNS_FLUSH=%d\nRAM_OPTIMIZE=%d\nRECYCLE_BIN=%d\n",
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
	)
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
	}
}


