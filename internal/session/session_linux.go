package session

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func pidIsCopilot(pid int) bool {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	return err == nil && bytes.Contains(b, []byte("copilot"))
}

func ancestorsFrom(pid int) map[int]bool {
	pids := map[int]bool{}
	for pid > 1 {
		pids[pid] = true
		b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if err != nil {
			break
		}
		i := bytes.LastIndexByte(b, ')')
		if i < 0 {
			break
		}
		fields := strings.Fields(string(b[i+1:]))
		if len(fields) < 2 {
			break
		}
		if pid, err = strconv.Atoi(fields[1]); err != nil {
			break
		}
	}
	return pids
}
