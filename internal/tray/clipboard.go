package tray

import (
	"encoding/binary"
	"errors"
	"unicode/utf16"
)

type clipboardAPI interface {
	alloc(size int) uintptr
	write(handle uintptr, data []byte) bool
	free(handle uintptr)
	open(owner uintptr) bool
	empty() bool
	setUnicodeText(handle uintptr) bool
	close()
}

func setClipboardTextWith(api clipboardAPI, owner uintptr, text string) error {
	if text == "" {
		return nil
	}
	if owner == 0 {
		return errors.New("clipboard owner window is required")
	}
	units := utf16.Encode([]rune(text))
	units = append(units, 0)
	data := make([]byte, len(units)*2)
	for i, unit := range units {
		binary.LittleEndian.PutUint16(data[i*2:], unit)
	}
	return setClipboardDataWith(api, owner, data)
}

func setClipboardDataWith(api clipboardAPI, owner uintptr, data []byte) error {
	if owner == 0 {
		return errors.New("clipboard owner window is required")
	}
	if len(data) == 0 {
		return errors.New("clipboard text is empty")
	}
	handle := api.alloc(len(data))
	if handle == 0 {
		return errors.New("clipboard memory allocation failed")
	}
	transferred := false
	defer func() {
		if !transferred {
			api.free(handle)
		}
	}()
	if !api.write(handle, data) {
		return errors.New("clipboard memory write failed")
	}
	if !api.open(owner) {
		return errors.New("open clipboard failed")
	}
	defer api.close()
	if !api.empty() {
		return errors.New("empty clipboard failed")
	}
	if !api.setUnicodeText(handle) {
		return errors.New("set clipboard data failed")
	}
	transferred = true
	return nil
}
