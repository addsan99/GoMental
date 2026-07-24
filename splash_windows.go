//go:build windows

package main

// Native pre-render splash for Windows.
//
// Wails does not show its window until WebView2 has initialised and loaded the
// SPA shell, which the startup log shows can take 1.6–5s (all of it inside
// wails.Run, none in our own code). During that window the user sees *nothing* —
// no blank frame, no taskbar paint. The in-webview #gm-splash in index.html
// can't help because the webview isn't up yet.
//
// So we paint a real Win32 window with the GoMental logo the instant runDesktop
// starts (main() runs at +0.001s), on its own OS thread with its own message
// pump, and tear it down at OnDomReady — the moment the real UI first paints.
// Everything here is best-effort and wrapped so it can never delay or crash the
// launch; if anything fails we simply proceed without a splash.

import (
	"bytes"
	_ "embed"
	"image/png"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

//go:embed frontend/public/gomental-lockup-rainbow-light.png
var splashLogoPNG []byte

// Win32 message / style / flag constants used below.
const (
	wsPopup         = 0x80000000
	wsExTopmost     = 0x00000008
	wsExToolwindow  = 0x00000080
	swShowNoActive  = 4
	csHRedraw       = 0x0002
	csVRedraw       = 0x0001
	idcArrow        = 32512
	wmDestroy       = 0x0002
	wmClose         = 0x0010
	wmPaint         = 0x000F
	wmEraseBkgnd    = 0x0014
	wmTimer         = 0x0113
	smCXScreen      = 0
	smCYScreen      = 1
	dibRGBColors    = 0
	srcCopy         = 0x00CC0020
	splashSafetyMS  = 15000
	splashTimerID   = 1
	creamColorRef   = 0x00EFF4F5 // COLORREF 0x00BBGGRR for #F5F4EF
)

var (
	splashUser32   = syscall.NewLazyDLL("user32.dll")
	splashGDI32    = syscall.NewLazyDLL("gdi32.dll")
	splashKernel32 = syscall.NewLazyDLL("kernel32.dll")

	procRegisterClassExW = splashUser32.NewProc("RegisterClassExW")
	procCreateWindowExW  = splashUser32.NewProc("CreateWindowExW")
	procDefWindowProcW   = splashUser32.NewProc("DefWindowProcW")
	procShowWindow       = splashUser32.NewProc("ShowWindow")
	procUpdateWindow     = splashUser32.NewProc("UpdateWindow")
	procGetMessageW      = splashUser32.NewProc("GetMessageW")
	procTranslateMessage = splashUser32.NewProc("TranslateMessage")
	procDispatchMessageW = splashUser32.NewProc("DispatchMessageW")
	procDestroyWindow    = splashUser32.NewProc("DestroyWindow")
	procPostQuitMessage  = splashUser32.NewProc("PostQuitMessage")
	procPostMessageW     = splashUser32.NewProc("PostMessageW")
	procBeginPaint       = splashUser32.NewProc("BeginPaint")
	procEndPaint         = splashUser32.NewProc("EndPaint")
	procFillRect         = splashUser32.NewProc("FillRect")
	procGetSystemMetrics = splashUser32.NewProc("GetSystemMetrics")
	procLoadCursorW      = splashUser32.NewProc("LoadCursorW")
	procSetTimer         = splashUser32.NewProc("SetTimer")

	procStretchDIBits    = splashGDI32.NewProc("StretchDIBits")
	procCreateSolidBrush = splashGDI32.NewProc("CreateSolidBrush")

	procGetModuleHandleW = splashKernel32.NewProc("GetModuleHandleW")

	splashWndProcPtr = syscall.NewCallback(splashWndProc)
)

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

type winMsg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      struct{ x, y int32 }
}

type winRect struct {
	left, top, right, bottom int32
}

type paintStruct struct {
	hdc         uintptr
	fErase      int32
	rcPaint     winRect
	fRestore    int32
	fIncUpdate  int32
	rgbReserved [32]byte
}

type bitmapInfoHeader struct {
	biSize          uint32
	biWidth         int32
	biHeight        int32
	biPlanes        uint16
	biBitCount      uint16
	biCompression   uint32
	biSizeImage     uint32
	biXPelsPerMeter int32
	biYPelsPerMeter int32
	biClrUsed       uint32
	biClrImportant  uint32
}

// Splash state. Only the splash thread reads the drawing fields; hwnd/close are
// guarded because closeSplash is called from a different (Wails) thread.
var (
	splashMu             sync.Mutex
	splashStarted        bool
	splashHWND           uintptr
	splashCloseRequested bool

	splashBrush uintptr
	splashBMI   bitmapInfoHeader
	splashBits  []byte // top-down 32-bit BGRA, logo flattened over cream

	splashSrcW, splashSrcH int32 // source image pixels
	splashWinW, splashWinH int32 // splash window (== client) size
	splashDstX, splashDstY int32 // logo blit origin within the window
	splashDstW, splashDstH int32 // logo blit size (scaled to fit)
)

// showSplash paints the native splash on its own thread. Safe to call once; a
// second call is a no-op. Returns immediately — the message pump runs in the
// background goroutine.
func showSplash() {
	splashMu.Lock()
	if splashStarted {
		splashMu.Unlock()
		return
	}
	splashStarted = true
	splashMu.Unlock()

	if !prepSplashImage() {
		return // image decode/layout failed — launch without a splash
	}
	go splashThread()
}

// closeSplash dismisses the splash. Called from the Wails thread at OnDomReady.
// If the splash window isn't up yet, it records the request so the splash thread
// tears down as soon as it creates the window.
func closeSplash() {
	splashMu.Lock()
	hwnd := splashHWND
	splashCloseRequested = true
	splashMu.Unlock()
	if hwnd != 0 {
		procPostMessageW.Call(hwnd, wmClose, 0, 0)
	}
}

// prepSplashImage decodes the embedded logo, flattens its alpha over the cream
// splash background into a top-down BGRA buffer, and computes the window/logo
// geometry. Returns false on any failure.
func prepSplashImage() (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	img, err := png.Decode(bytes.NewReader(splashLogoPNG))
	if err != nil {
		return false
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return false
	}

	const cr, cg, cb = 0xF5, 0xF4, 0xEF // cream background, matches window bg
	bits := make([]byte, w*h*4)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			// color.RGBA() returns alpha-premultiplied 16-bit components, so the
			// composite over cream is simply premult + cream*(1-alpha).
			r16, g16, bl16, a16 := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			inv := uint32(255) - a16>>8
			outR := r16>>8 + cr*inv/255
			outG := g16>>8 + cg*inv/255
			outB := bl16>>8 + cb*inv/255
			i := (y*w + x) * 4
			bits[i+0] = byte(outB)
			bits[i+1] = byte(outG)
			bits[i+2] = byte(outR)
			bits[i+3] = 0xFF
		}
	}

	splashBits = bits
	splashSrcW = int32(w)
	splashSrcH = int32(h)
	splashBMI = bitmapInfoHeader{
		biSize:      uint32(unsafe.Sizeof(bitmapInfoHeader{})),
		biWidth:     int32(w),
		biHeight:    -int32(h), // negative => top-down rows
		biPlanes:    1,
		biBitCount:  32,
		biCompression: 0, // BI_RGB
	}

	// Fit the logo into a 340x200 box (no upscaling past native), pad by 48px.
	const maxW, maxH, pad = 340.0, 200.0, 48
	scale := minFloat(maxW/float64(w), maxH/float64(h))
	if scale > 1 {
		scale = 1
	}
	splashDstW = int32(float64(w)*scale + 0.5)
	splashDstH = int32(float64(h)*scale + 0.5)
	splashWinW = splashDstW + 2*pad
	splashWinH = splashDstH + 2*pad
	splashDstX = pad
	splashDstY = pad
	return true
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// splashThread owns the splash window: it registers the class, creates and shows
// the window, and runs a dedicated message loop until dismissed. Runs on its own
// locked OS thread so it never contends with Wails' main-thread message pump.
func splashThread() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer func() { _ = recover() }() // never let a bad syscall take down the app

	hInstance, _, _ := procGetModuleHandleW.Call(0)
	splashBrush, _, _ = procCreateSolidBrush.Call(creamColorRef)
	cursor, _, _ := procLoadCursorW.Call(0, idcArrow)
	className, _ := syscall.UTF16PtrFromString("GoMentalSplashWindow")
	title, _ := syscall.UTF16PtrFromString("GoMental")

	wc := wndClassExW{
		style:         csHRedraw | csVRedraw,
		lpfnWndProc:   splashWndProcPtr,
		hInstance:     hInstance,
		hCursor:       cursor,
		hbrBackground: splashBrush,
		lpszClassName: className,
	}
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	if atom, _, _ := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); atom == 0 {
		return
	}

	screenW, _, _ := procGetSystemMetrics.Call(smCXScreen)
	screenH, _, _ := procGetSystemMetrics.Call(smCYScreen)
	x := (int32(screenW) - splashWinW) / 2
	y := (int32(screenH) - splashWinH) / 2

	hwnd, _, _ := procCreateWindowExW.Call(
		wsExTopmost|wsExToolwindow,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(title)),
		wsPopup,
		uintptr(x), uintptr(y), uintptr(splashWinW), uintptr(splashWinH),
		0, 0, hInstance, 0,
	)
	if hwnd == 0 {
		return
	}

	// Publish the handle; if a close was requested before we got here, honour it.
	splashMu.Lock()
	if splashCloseRequested {
		splashMu.Unlock()
		procDestroyWindow.Call(hwnd)
		return
	}
	splashHWND = hwnd
	splashMu.Unlock()

	procShowWindow.Call(hwnd, swShowNoActive)
	procUpdateWindow.Call(hwnd)
	// Safety net: self-dismiss if OnDomReady never arrives (e.g. a load error),
	// so the splash can never get stuck on screen forever.
	procSetTimer.Call(hwnd, splashTimerID, splashSafetyMS, 0)

	var m winMsg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 { // 0 = WM_QUIT, -1 = error
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}

	splashMu.Lock()
	splashHWND = 0
	splashMu.Unlock()
}

func splashWndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmPaint:
		var ps paintStruct
		hdc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		rc := winRect{0, 0, splashWinW, splashWinH}
		procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rc)), splashBrush)
		if len(splashBits) > 0 {
			procStretchDIBits.Call(
				hdc,
				uintptr(splashDstX), uintptr(splashDstY), uintptr(splashDstW), uintptr(splashDstH),
				0, 0, uintptr(splashSrcW), uintptr(splashSrcH),
				uintptr(unsafe.Pointer(&splashBits[0])),
				uintptr(unsafe.Pointer(&splashBMI)),
				dibRGBColors, srcCopy,
			)
		}
		procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		return 0
	case wmEraseBkgnd:
		return 1 // painted in WM_PAINT; skip default erase to avoid flicker
	case wmTimer:
		procDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return r
}
