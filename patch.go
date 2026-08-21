package main

import (
	"fmt"
	"io"
	"os"
)

// Browser args embedded in yuanbao.exe (fixed-length in-place patch).
var (
	patchOld = []byte("--disable-features=msWebOOUI,msPdfOOUI,msSmartScreenProtection --autoplay-policy=no-user-gesture-required")
	patchNew = []byte("--remote-debugging-port=9333 --disable-features=msWebOOUI      --autoplay-policy=no-user-gesture-required")
)

func ensureDebugExe(cfg Config) error {
	src := cfg.YuanbaoExe()
	dst := cfg.DebugExe()

	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("yuanbao.exe not found: %s", src)
	}

	needPatch := true
	if info, err := os.Stat(dst); err == nil {
		srcInfo, err2 := os.Stat(src)
		if err2 == nil && !srcInfo.ModTime().After(info.ModTime()) && debugExeHasCurrentPatch(dst) {
			needPatch = false
		}
	}
	if !needPatch {
		return nil
	}

	if err := copyAndPatch(src, dst); err != nil {
		return err
	}
	fmt.Printf("[patch] created %s\n", dst)
	return nil
}

func copyAndPatch(src, dst string) error {
	if len(patchOld) != len(patchNew) {
		return fmt.Errorf("internal patch length mismatch")
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	data, err := io.ReadAll(in)
	if err != nil {
		return err
	}

	idx := indexOf(data, patchOld)
	if idx < 0 {
		return fmt.Errorf("browser-args string not found in %s (version mismatch?)", src)
	}
	copy(data[idx:], patchNew)

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := out.Write(data); err != nil {
		return err
	}
	return out.Close()
}

func debugExeHasCurrentPatch(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return indexOf(data, patchNew) >= 0
}

func indexOf(data, sub []byte) int {
	for i := 0; i+len(sub) <= len(data); i++ {
		if bytesEqual(data[i:i+len(sub)], sub) {
			return i
		}
	}
	return -1
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
