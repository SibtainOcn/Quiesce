package main

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modshell32Rb           = windows.NewLazySystemDLL("shell32.dll")
	procSHEmptyRecycleBinW = modshell32Rb.NewProc("SHEmptyRecycleBinW")
	procSHQueryRecycleBinW = modshell32Rb.NewProc("SHQueryRecycleBinW")
)

const (
	SHERB_NOCONFIRMATION = 0x00000001
	SHERB_NOPROGRESSUI   = 0x00000002
	SHERB_NOSOUND        = 0x00000004
)

// SHQUERYRBINFO mirrors the Win32 struct used by SHQueryRecycleBinW.
// On 64-bit Windows the native C struct has 4 bytes of padding after
// cbSize so the following __int64 fields land on an 8-byte boundary.
// Go's own alignment rules insert the same padding automatically for
// a uint32 followed by an int64, so this would lay out correctly even
// without the explicit field below - it's kept here anyway to make the
// ABI layout self-documenting and safe against future field reordering.
// cbSize must be set to sizeof(SHQUERYRBINFO) before the call, per the
// Win32 API contract (same pattern as MEMORYSTATUSEX.Length elsewhere
// in this codebase).
type SHQUERYRBINFO struct {
	CbSize      uint32
	_padding    uint32 // matches native alignment; see comment above
	I64Size     int64
	I64NumItems int64
}

// QueryRecycleBinSize returns the total number of bytes currently held
// in the Recycle Bin across all drives. Passing 0 (NULL) as the root
// path queries all drives combined, per the SHQueryRecycleBinW docs.
// Returns (0, false) if the query fails.
func QueryRecycleBinSize() (uint64, bool) {
	var info SHQUERYRBINFO
	info.CbSize = uint32(unsafe.Sizeof(info))

	r1, _, _ := procSHQueryRecycleBinW.Call(
		0, // pszRootPath - nil means all drives
		uintptr(unsafe.Pointer(&info)),
	)

	// S_OK (0) means info was filled in successfully.
	if uint32(r1) != 0 {
		return 0, false
	}
	if info.I64Size < 0 {
		return 0, false
	}
	return uint64(info.I64Size), true
}

// EmptyRecycleBin empties the Recycle Bin for all drives on the system.
// It first queries the current size so the caller can report how much
// was actually cleared, then empties the bin.
//
// Returns (bytesFreed, ok). ok is true on success (including the
// "already empty" case, which Windows reports via non-zero HRESULTs
// that don't indicate a real failure). bytesFreed is best-effort: if
// the size query fails, it is 0 even though the empty operation may
// still have succeeded.
func EmptyRecycleBin() (uint64, bool) {
	sizeBefore, sizeOk := QueryRecycleBinSize()

	flags := uintptr(SHERB_NOCONFIRMATION | SHERB_NOPROGRESSUI | SHERB_NOSOUND)

	r1, _, _ := procSHEmptyRecycleBinW.Call(
		0, // hwnd - no owner window
		0, // pszRootPath - nil means all drives
		flags,
	)

	// SHEmptyRecycleBinW returns an HRESULT. S_OK (0) is success.
	// S_FALSE and the "not found" HRESULT can occur when the bin is
	// already empty on every drive - treat these as success too, since
	// there's nothing wrong, just nothing to clean.
	var ok bool
	switch uint32(r1) {
	case 0x00000000, // S_OK
		0x00000001, // S_FALSE - nothing to do
		0x80070002: // ERROR_FILE_NOT_FOUND (HRESULT form) - already empty
		ok = true
	default:
		ok = false
	}

	if !ok || !sizeOk {
		return 0, ok
	}
	return sizeBefore, true
}

// FormatBytesHuman converts a byte count into a human-readable string
// using binary (1024-based) units, matching how Windows itself reports
// file and folder sizes (KB/MB/GB, not KiB/MiB/GiB, for familiarity).
func FormatBytesHuman(bytes uint64) string {
	const unit = 1024.0
	if bytes < uint64(unit) {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := unit, 0
	units := []string{"KB", "MB", "GB", "TB", "PB"}
	f := float64(bytes)
	for f/div >= unit && exp < len(units)-1 {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %s", f/div, units[exp])
}
