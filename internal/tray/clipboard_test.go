package tray

import (
	"testing"
)

type clipboardFake struct {
	allocOK bool
	writeOK bool
	openOK  bool
	emptyOK bool
	setOK   bool
	freed   int
	opened  int
	closed  int
	owner   uintptr
	data    []byte
}

func (f *clipboardFake) alloc(int) uintptr {
	if f.allocOK {
		return 1
	}
	return 0
}
func (f *clipboardFake) write(_ uintptr, data []byte) bool {
	f.data = append([]byte(nil), data...)
	return f.writeOK
}
func (f *clipboardFake) free(uintptr) { f.freed++ }
func (f *clipboardFake) open(owner uintptr) bool {
	f.owner = owner
	if f.openOK {
		f.opened++
		return true
	}
	return false
}
func (f *clipboardFake) empty() bool                 { return f.emptyOK }
func (f *clipboardFake) setUnicodeText(uintptr) bool { return f.setOK }
func (f *clipboardFake) close()                      { f.closed++ }

func clipboardFakeReady() *clipboardFake {
	return &clipboardFake{allocOK: true, writeOK: true, openOK: true, emptyOK: true, setOK: true}
}

func TestSetClipboardTextWithRequiresWindowOwner(t *testing.T) {
	fake := clipboardFakeReady()
	err := setClipboardTextWith(fake, 0, "fixture")
	if err == nil || fake.freed != 0 || fake.opened != 0 {
		t.Fatalf("owner check should fail before allocation/open: err=%v fake=%+v", err, fake)
	}
}

func TestSetClipboardDataWithCleansOwnedMemoryOnEachFailure(t *testing.T) {
	cases := []struct {
		name string
		set  func(*clipboardFake)
	}{
		{"allocate", func(f *clipboardFake) { f.allocOK = false }},
		{"write", func(f *clipboardFake) { f.writeOK = false }},
		{"open", func(f *clipboardFake) { f.openOK = false }},
		{"empty", func(f *clipboardFake) { f.emptyOK = false }},
		{"set", func(f *clipboardFake) { f.setOK = false }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := clipboardFakeReady()
			tc.set(fake)
			if err := setClipboardDataWith(fake, 7, []byte{1, 2}); err == nil {
				t.Fatal("expected operation failure")
			}
			wantFree := 1
			if tc.name == "allocate" {
				wantFree = 0
			}
			if fake.freed != wantFree {
				t.Fatalf("freed=%d want %d", fake.freed, wantFree)
			}
			if fake.opened != fake.closed {
				t.Fatalf("opened=%d closed=%d", fake.opened, fake.closed)
			}
		})
	}
}

func TestSetClipboardDataWithTransfersMemoryOnlyOnSuccess(t *testing.T) {
	fake := clipboardFakeReady()
	if err := setClipboardTextWith(fake, 7, "猫"); err != nil {
		t.Fatal(err)
	}
	if fake.freed != 0 || fake.opened != 1 || fake.closed != 1 || fake.owner != 7 {
		t.Fatalf("unexpected ownership/lifecycle: %+v", fake)
	}
	if len(fake.data) != 4 { // 猫's UTF-16 code unit plus NUL terminator.
		t.Fatalf("UTF-16 payload bytes=%d want 4", len(fake.data))
	}
}
