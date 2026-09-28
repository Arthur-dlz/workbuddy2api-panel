//go:build windows

package tray

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	pRegisterClassExW    = user32.NewProc("RegisterClassExW")
	pCreateWindowExW     = user32.NewProc("CreateWindowExW")
	pDefWindowProcW      = user32.NewProc("DefWindowProcW")
	pGetMessageW         = user32.NewProc("GetMessageW")
	pTranslateMessage    = user32.NewProc("TranslateMessage")
	pDispatchMessageW    = user32.NewProc("DispatchMessageW")
	pPostQuitMessage     = user32.NewProc("PostQuitMessage")
	pDestroyWindow       = user32.NewProc("DestroyWindow")
	pCreatePopupMenu     = user32.NewProc("CreatePopupMenu")
	pAppendMenuW         = user32.NewProc("AppendMenuW")
	pTrackPopupMenu      = user32.NewProc("TrackPopupMenu")
	pDestroyMenu         = user32.NewProc("DestroyMenu")
	pGetCursorPos        = user32.NewProc("GetCursorPos")
	pSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	pPostMessageW        = user32.NewProc("PostMessageW")
	pLoadIconW           = user32.NewProc("LoadIconW")
	pOpenClipboard       = user32.NewProc("OpenClipboard")
	pEmptyClipboard      = user32.NewProc("EmptyClipboard")
	pSetClipboardData    = user32.NewProc("SetClipboardData")
	pCloseClipboard      = user32.NewProc("CloseClipboard")

	pShell_NotifyIconW = shell32.NewProc("Shell_NotifyIconW")
	pGetModuleHandleW  = kernel32.NewProc("GetModuleHandleW")
	pGlobalAlloc       = kernel32.NewProc("GlobalAlloc")
	pGlobalLock        = kernel32.NewProc("GlobalLock")
	pGlobalUnlock      = kernel32.NewProc("GlobalUnlock")
)

const (
	wmUser        = 0x0400
	wmTrayIcon    = wmUser + 101
	wmLButtonDbl  = 0x0203
	wmRButtonUp   = 0x0205
	wmCommand     = 0x0111
	wmClose       = 0x0010
	wmDestroy     = 0x0002

	nimAdd        = 0x00000000
	nimModify     = 0x00000001
	nimDelete     = 0x00000002
	nimSetVersion = 0x00000004

	nifMessage    = 0x00000001
	nifIcon       = 0x00000002
	nifTip        = 0x00000004
	nifInfo       = 0x00000010

	tpmRightButton = 0x0002
	mfString       = 0x0000
	mfSeparator    = 0x0800

	idiApplication = 32512
	cfUnicodetext  = 13
	gmemMoveable   = 0x0002
)

type point struct {
	x, y int32
}

type wndClassExW struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

type notifyIconDataW struct {
	cbSize           uint32
	hWnd             uintptr
	uID              uint32
	uFlags           uint32
	uCallbackMessage uint32
	hIcon            uintptr
	szTip            [128]uint16
	dwState          uint32
	dwStateMask      uint32
	szInfo           [256]uint16
	uVersion         uint32
	szInfoTitle      [64]uint16
	dwInfoFlags      uint32
	guidItem         [16]byte
	hBalloonIcon     uintptr
}

type msg struct {
	hwnd     uintptr
	message  uint32
	wParam   uintptr
	lParam   uintptr
	time     uint32
	pt       point
	lPrivate uint32
}

// Config 托盘参数。
type Config struct {
	Title       string
	Port        int
	APIKey      string
	OnExit      func()
}

const (
	cmdOpenPanel = 1001
	cmdCopyURL   = 1002
	cmdCopyKey   = 1003
	cmdOpenDir   = 1004
	cmdExit      = 1005
)

var (
	currentConfig Config
	globalHWnd    uintptr
	nid           notifyIconDataW
)

// Run 启动托盘消息循环（阻塞当前 goroutine，通常放在独立 goroutine 或主线程）。
func Run(cfg Config) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	currentConfig = cfg
	hInstance, _, _ := pGetModuleHandleW.Call(0)

	className, err := syscall.UTF16PtrFromString("WorkBuddyGatewayTrayClass")
	if err != nil {
		return err
	}
	windowName, err := syscall.UTF16PtrFromString("WorkBuddyGatewayTray")
	if err != nil {
		return err
	}

	hIcon, _, _ := pLoadIconW.Call(0, uintptr(idiApplication))

	wndClass := wndClassExW{
		cbSize:      uint32(unsafe.Sizeof(wndClassExW{})),
		lpfnWndProc: syscall.NewCallback(wndProc),
		hInstance:   hInstance,
		hIcon:       hIcon,
		lpszClassName: className,
	}

	if ret, _, _ := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wndClass))); ret == 0 {
		// 允许重复注册
	}

	hWnd, _, _ := pCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windowName)),
		0,
		0, 0, 0, 0,
		0, 0, hInstance, 0,
	)
	if hWnd == 0 {
		return fmt.Errorf("CreateWindowExW failed")
	}
	globalHWnd = hWnd

	nid = notifyIconDataW{
		cbSize:           uint32(unsafe.Sizeof(notifyIconDataW{})),
		hWnd:             hWnd,
		uID:              1,
		uFlags:           nifMessage | nifIcon | nifTip | nifInfo,
		uCallbackMessage: wmTrayIcon,
		hIcon:            hIcon,
	}

	tip := fmt.Sprintf("WorkBuddy 网关 (:%d)", cfg.Port)
	if cfg.Title != "" {
		tip = fmt.Sprintf("%s (:%d)", cfg.Title, cfg.Port)
	}
	copy(nid.szTip[:], utf16String(tip))

	balloonTitle := "WorkBuddy 网关运行中"
	balloonText := fmt.Sprintf("服务已在后台运行 (端口 %d)\n双击图标打开控制面板", cfg.Port)
	copy(nid.szInfoTitle[:], utf16String(balloonTitle))
	copy(nid.szInfo[:], utf16String(balloonText))
	nid.dwInfoFlags = 1 // NIIF_INFO

	pShell_NotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&nid)))

	var m msg
	for {
		ret, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(ret) <= 0 {
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}

	pShell_NotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
	return nil
}

// Stop 关闭托盘
func Stop() {
	if globalHWnd != 0 {
		pShell_NotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
		pPostMessageW.Call(globalHWnd, wmClose, 0, 0)
	}
}

func wndProc(hWnd, uMsg, wParam, lParam uintptr) uintptr {
	switch uMsg {
	case wmTrayIcon:
		switch lParam {
		case wmLButtonDbl:
			openWebPanel()
		case wmRButtonUp:
			showContextMenu(hWnd)
		}
		return 0

	case wmCommand:
		switch wParam {
		case cmdOpenPanel:
			openWebPanel()
		case cmdCopyURL:
			url := fmt.Sprintf("http://127.0.0.1:%d/v1", currentConfig.Port)
			setClipboardText(url)
		case cmdCopyKey:
			setClipboardText(currentConfig.APIKey)
		case cmdOpenDir:
			dir, _ := os.Getwd()
			exec.Command("explorer.exe", dir).Start()
		case cmdExit:
			pShell_NotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
			if currentConfig.OnExit != nil {
				currentConfig.OnExit()
			}
			pPostQuitMessage.Call(0)
			os.Exit(0)
		}
		return 0

	case wmClose:
		pShell_NotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
		pDestroyWindow.Call(hWnd)
		return 0

	case wmDestroy:
		pPostQuitMessage.Call(0)
		return 0
	}

	ret, _, _ := pDefWindowProcW.Call(hWnd, uMsg, wParam, lParam)
	return ret
}

func showContextMenu(hWnd uintptr) {
	hMenu, _, _ := pCreatePopupMenu.Call()
	if hMenu == 0 {
		return
	}
	defer pDestroyMenu.Call(hMenu)

	titleStr := fmt.Sprintf("WorkBuddy 网关 (:%d)", currentConfig.Port)
	appendMenuItem(hMenu, 0, titleStr, true)
	appendSeparator(hMenu)
	appendMenuItem(hMenu, cmdOpenPanel, "🌐 打开管理面板", false)
	appendMenuItem(hMenu, cmdCopyURL, "📋 复制 API 端点 (URL)", false)
	appendMenuItem(hMenu, cmdCopyKey, "🔑 复制 API 密钥 (Key)", false)
	appendMenuItem(hMenu, cmdOpenDir, "📁 打开工作目录", false)
	appendSeparator(hMenu)
	appendMenuItem(hMenu, cmdExit, "❌ 退出网关服务", false)

	var pt point
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	pSetForegroundWindow.Call(hWnd)

	pTrackPopupMenu.Call(
		hMenu,
		tpmRightButton,
		uintptr(pt.x),
		uintptr(pt.y),
		0,
		hWnd,
		0,
	)
}

func appendMenuItem(hMenu uintptr, id uintptr, text string, disabled bool) {
	flags := uintptr(mfString)
	if disabled {
		flags |= 0x0001 // MF_GRAYED
	}
	ptr, _ := syscall.UTF16PtrFromString(text)
	pAppendMenuW.Call(hMenu, flags, id, uintptr(unsafe.Pointer(ptr)))
}

func appendSeparator(hMenu uintptr) {
	pAppendMenuW.Call(hMenu, mfSeparator, 0, 0)
}

func openWebPanel() {
	url := fmt.Sprintf("http://127.0.0.1:%d/panel/", currentConfig.Port)
	exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}

func utf16String(s string) []uint16 {
	res, _ := syscall.UTF16FromString(s)
	return res
}

func setClipboardText(text string) {
	if text == "" {
		return
	}
	utf16, err := syscall.UTF16FromString(text)
	if err != nil {
		return
	}
	bytes := len(utf16) * 2

	hMem, _, _ := pGlobalAlloc.Call(gmemMoveable, uintptr(bytes))
	if hMem == 0 {
		return
	}
	ptr, _, _ := pGlobalLock.Call(hMem)
	if ptr == 0 {
		return
	}
	copy((*[1 << 20]byte)(unsafe.Pointer(ptr))[:bytes], (*[1 << 20]byte)(unsafe.Pointer(&utf16[0]))[:bytes])
	pGlobalUnlock.Call(hMem)

	pOpenClipboard.Call(0)
	pEmptyClipboard.Call()
	pSetClipboardData.Call(cfUnicodetext, hMem)
	pCloseClipboard.Call()
}
