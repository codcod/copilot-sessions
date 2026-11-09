package session

import (
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// pidIsCopilot reports whether pid's command line contains "copilot" in any case (Windows
// resolves commands case-insensitively, and the command line keeps what was typed), matching argv like
// linux (cmdline) and darwin (procargs2): it covers a native copilot.exe and a node launch.
// Any error (access denied, a gone PID) reads as false.
func pidIsCopilot(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var n uint32
	windows.NtQueryInformationProcess(h, windows.ProcessCommandLineInformation, nil, 0, &n)
	if n < uint32(unsafe.Sizeof(windows.NTUnicodeString{})) {
		return false
	}
	buf := make([]byte, n)
	if windows.NtQueryInformationProcess(h, windows.ProcessCommandLineInformation, unsafe.Pointer(&buf[0]), n, &n) != nil {
		return false
	}
	return strings.Contains(strings.ToLower((*windows.NTUnicodeString)(unsafe.Pointer(&buf[0])).String()), "copilot")
}

// ancestorsFrom walks parent PIDs from one Toolhelp32 snapshot. It stops on a missing PID,
// Idle/System (0/4), a self-parent or a revisit, since Windows reuses PIDs freely.
// ponytail: a reused parent PID can add an unrelated ancestor; that only moves the
// "(this session)" marker, so creation times are not compared.
func ancestorsFrom(pid int) map[int]bool {
	pids := map[int]bool{pid: true}
	parent := map[int]int{}
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return pids
	}
	defer windows.CloseHandle(snap)
	e := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		parent[int(e.ProcessID)] = int(e.ParentProcessID)
	}
	for {
		ppid, ok := parent[pid]
		if !ok || ppid == 0 || ppid == 4 || pids[ppid] {
			return pids
		}
		pids[ppid] = true
		pid = ppid
	}
}
