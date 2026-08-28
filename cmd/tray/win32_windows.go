//go:build windows

package main

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	pRegisterClassExW  = user32.NewProc("RegisterClassExW")
	pCreateWindowExW   = user32.NewProc("CreateWindowExW")
	pDefWindowProcW    = user32.NewProc("DefWindowProcW")
	pGetMessageW       = user32.NewProc("GetMessageW")
	pTranslateMessage  = user32.NewProc("TranslateMessage")
	pDispatchMessageW  = user32.NewProc("DispatchMessageW")
	pPostQuitMessage   = user32.NewProc("PostQuitMessage")
	pCreatePopupMenu   = user32.NewProc("CreatePopupMenu")
	pAppendMenuW       = user32.NewProc("AppendMenuW")
	pTrackPopupMenu    = user32.NewProc("TrackPopupMenu")
	pDestroyMenu       = user32.NewProc("DestroyMenu")
	pGetCursorPos      = user32.NewProc("GetCursorPos")
	pSetForegroundWin  = user32.NewProc("SetForegroundWindow")
	pPostMessageW      = user32.NewProc("PostMessageW")
	pMessageBoxW       = user32.NewProc("MessageBoxW")
	pCreateIconIndirect = user32.NewProc("CreateIconIndirect")

	pShellNotifyIconW = shell32.NewProc("Shell_NotifyIconW")
	pShellExecuteW    = shell32.NewProc("ShellExecuteW")
	pSetAppID         = shell32.NewProc("SetCurrentProcessExplicitAppUserModelID")

	pCreateBitmap     = gdi32.NewProc("CreateBitmap")
	pCreateDIBSection = gdi32.NewProc("CreateDIBSection")
	pDeleteObject     = gdi32.NewProc("DeleteObject")

	pGetModuleHandleW         = kernel32.NewProc("GetModuleHandleW")
	pGetUserDefaultUILanguage = kernel32.NewProc("GetUserDefaultUILanguage")
)

const (
	wmDestroy  = 0x0002
	wmClose    = 0x0010
	wmCommand  = 0x0111
	wmApp      = 0x8000
	wmTrayIcon = wmApp + 1

	wmRButtonUp     = 0x0205
	wmLButtonUp     = 0x0202
	wmLButtonDblClk = 0x0203

	nimAdd    = 0x0
	nimModify = 0x1
	nimDelete = 0x2

	// NIM_SETVERSION is deliberately absent. It was needed for NIIF_USER (our
	// own icon in the balloon notification), which we had to give up on, and
	// the side effect of version 4 is that the shell stops showing the hover
	// tooltip by itself.

	nifMessage = 0x1
	nifIcon    = 0x2
	nifTip     = 0x4
	nifInfo    = 0x10

	niifInfo    = 0x1
	niifWarning = 0x2

	mfString    = 0x0
	mfSeparator = 0x800
	mfPopup     = 0x10
	mfGrayed    = 0x1
	mfChecked   = 0x8

	tpmRightButton = 0x2
	tpmReturnCmd   = 0x100

	mbOk          = 0x0
	mbYesNo       = 0x4
	mbIconWarning = 0x30
	mbIconInfo    = 0x40
	idYes         = 6

	swHide = 0
	swShow = 5
)

type point struct{ X, Y int32 }

type msgT struct {
	HWnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}

type wndClassEx struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

type notifyIconData struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         windows.GUID
	HBalloonIcon     uintptr
}

type iconInfo struct {
	FIcon    int32
	XHotspot uint32
	YHotspot uint32
	HbmMask  uintptr
	HbmColor uintptr
}

type bitmapInfoHeader struct {
	BiSize          uint32
	BiWidth         int32
	BiHeight        int32
	BiPlanes        uint16
	BiBitCount      uint16
	BiCompression   uint32
	BiSizeImage     uint32
	BiXPelsPerMeter int32
	BiYPelsPerMeter int32
	BiClrUsed       uint32
	BiClrImportant  uint32
}

func utf16(s string) *uint16 {
	p, _ := windows.UTF16PtrFromString(s)
	return p
}

func copyToArray(dst []uint16, s string) {
	src := windows.StringToUTF16(s)
	if len(src) > len(dst) {
		src = src[:len(dst)]
		src[len(src)-1] = 0
	}
	copy(dst, src)
}

// makeIcon draws the tray icon directly in memory.
//
// That way we need no .ico file, no resource compiler and no extra build step,
// and the color can carry the state: green - everything is in place, orange -
// it is not.
func makeIcon(ok bool) uintptr {
	const size = 32
	pixels := make([]uint32, size*size)

	var r, g, b uint32
	if ok {
		r, g, b = 0x2e, 0xa0, 0x43 // green
	} else {
		r, g, b = 0xd2, 0x7a, 0x0d // orange
	}

	inRounded := func(x, y int) bool {
		const rad = 6
		if x < rad && y < rad {
			return (rad-x)*(rad-x)+(rad-y)*(rad-y) <= rad*rad
		}
		if x >= size-rad && y < rad {
			dx := x - (size - rad - 1)
			return dx*dx+(rad-y)*(rad-y) <= rad*rad
		}
		if x < rad && y >= size-rad {
			dy := y - (size - rad - 1)
			return (rad-x)*(rad-x)+dy*dy <= rad*rad
		}
		if x >= size-rad && y >= size-rad {
			dx, dy := x-(size-rad-1), y-(size-rad-1)
			return dx*dx+dy*dy <= rad*rad
		}
		return true
	}

	// three bars of different heights - still reads as "sound" even at 16 pixels
	bars := [][3]int{{7, 11, 21}, {14, 6, 26}, {21, 10, 22}}

	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			if !inRounded(x, y) {
				pixels[y*size+x] = 0
				continue
			}
			cr, cg, cb := r, g, b
			for _, bar := range bars {
				bx, top, bottom := bar[0], bar[1], bar[2]
				if x >= bx && x < bx+4 && y >= top && y < bottom {
					cr, cg, cb = 0xff, 0xff, 0xff
				}
			}
			// BGRA; premultiplied alpha is not needed at full opacity
			pixels[y*size+x] = 0xff000000 | (cr << 16) | (cg << 8) | cb
		}
	}

	bi := bitmapInfoHeader{
		BiSize:        uint32(unsafe.Sizeof(bitmapInfoHeader{})),
		BiWidth:       size,
		BiHeight:      -size, // top-down
		BiPlanes:      1,
		BiBitCount:    32,
		BiCompression: 0,
	}
	var bits unsafe.Pointer
	hbmColor, _, _ := pCreateDIBSection.Call(0, uintptr(unsafe.Pointer(&bi)), 0,
		uintptr(unsafe.Pointer(&bits)), 0, 0)
	if hbmColor == 0 {
		return 0
	}
	dst := unsafe.Slice((*uint32)(bits), size*size)
	copy(dst, pixels)

	hbmMask, _, _ := pCreateBitmap.Call(size, size, 1, 1, 0)

	ii := iconInfo{FIcon: 1, HbmMask: hbmMask, HbmColor: hbmColor}
	hIcon, _, _ := pCreateIconIndirect.Call(uintptr(unsafe.Pointer(&ii)))

	pDeleteObject.Call(hbmColor)
	pDeleteObject.Call(hbmMask)
	return hIcon
}

func messageBox(title, text string, flags uintptr) uintptr {
	r, _, _ := pMessageBoxW.Call(0,
		uintptr(unsafe.Pointer(utf16(text))),
		uintptr(unsafe.Pointer(utf16(title))), flags)
	return r
}

// shellExecute launches anything; the "runas" verb elevates, and that is the
// only place where the user will ever see a UAC prompt.
func shellExecute(verb, file, args string) bool {
	return shellExecuteShow(verb, file, args, swShow)
}

// shellExecuteShow - the same, but with an explicit window show mode.
//
// It exists for the sake of swHide: the helper PowerShell scripts run with
// administrator rights, and without it a black console window jumps out into
// the middle of the screen for a second. It shows nothing of what it is doing,
// it only scares the user.
func shellExecuteShow(verb, file, args string, show uintptr) bool {
	var v uintptr
	if verb != "" {
		v = uintptr(unsafe.Pointer(utf16(verb)))
	}
	r, _, _ := pShellExecuteW.Call(0, v,
		uintptr(unsafe.Pointer(utf16(file))),
		uintptr(unsafe.Pointer(utf16(args))), 0, show)
	return r > 32 // anything less than that is an error code
}

// setAppID gives the process an explicit identity for the notification system.
// Without it Windows puts the exe name into the title of the popup.
func setAppID(id string) {
	p, _ := windows.UTF16PtrFromString(id)
	pSetAppID.Call(uintptr(unsafe.Pointer(p)))
}