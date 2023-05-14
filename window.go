package wv2

import (
	"fmt"
	"log"
	"unsafe"

	"github.com/b1naryth1ef/wv2/pkg/edge"
	"github.com/b1naryth1ef/wv2/win32"
	"github.com/b1naryth1ef/wv2/winc"
	"github.com/b1naryth1ef/wv2/winc/w32"
)

type WindowOpts struct {
	Frameless      bool
	MinimizeOnQuit bool

	InitialURL    string
	InitialWidth  int
	InitialHeight int

	MaxWidth  int
	MaxHeight int

	DataPath              string
	BrowserPath           string
	AdditionalBrowserArgs []string
}

type Window struct {
	winc.Form

	Chromium *edge.Chromium

	opts   WindowOpts
	handle uintptr
}

func NewWindow(opts WindowOpts) *Window {
	chromium := edge.NewChromium()

	var exStyle = w32.WS_EX_CONTROLPARENT | w32.WS_EX_APPWINDOW
	var dwStyle = w32.WS_OVERLAPPEDWINDOW

	winc.RegClassOnlyOnce("wv2Window")
	handle := winc.CreateWindow("wv2Window", nil, uint(exStyle), uint(dwStyle))

	window := &Window{
		Chromium: chromium,
		handle:   handle,
		opts:     opts,
	}

	window.SetIsForm(true)
	window.SetHandle(handle)
	winc.RegMsgHandler(window)

	win32.ShowWindow(handle)
	w32.SetForegroundWindow(handle)
	w32.SetFocus(handle)

	width := opts.InitialWidth
	height := opts.InitialHeight

	if width == 0 {
		width = 800
	}
	if height == 0 {
		height = 600
	}

	window.SetSize(width, height)
	if opts.Frameless {

		win32.ExtendFrameIntoClientArea(handle, true)
	}

	additionalBrowserArgs := []string{"--enable-features=msWebView2EnableDraggableRegions"}
	if opts.AdditionalBrowserArgs != nil {
		additionalBrowserArgs = append(additionalBrowserArgs, opts.AdditionalBrowserArgs...)
	}

	chromium.Embed(handle, opts.DataPath, opts.BrowserPath, additionalBrowserArgs)
	chromium.Resize()

	chromium.SetGlobalPermission(edge.CoreWebView2PermissionStateAllow)
	chromium.AddWebResourceRequestedFilter("*", edge.COREWEBVIEW2_WEB_RESOURCE_CONTEXT_ALL)

	if opts.InitialURL != "" {
		chromium.Navigate(opts.InitialURL)
	}

	return window
}

func (w *Window) Run() {
	w.OnSize().Bind(func(arg *winc.Event) {
		if w.opts.Frameless {
			event, _ := arg.Data.(*winc.SizeEventData)
			if event != nil && event.Type == w32.SIZE_MINIMIZED {
				return
			}
		}

		w.Chromium.Resize()
	})

	w.OnClose().Bind(func(arg *winc.Event) {
		if w.opts.MinimizeOnQuit {
			w.Hide()
		} else {
			w.Quit()
		}
	})

	winc.RunMainLoop()
}

func (w *Window) Quit() {
	w.Invoke(winc.Exit)
}

func (w *Window) WndProc(msg uint32, wparam, lparam uintptr) uintptr {
	switch msg {
	case win32.WM_POWERBROADCAST:
		switch wparam {
		case win32.PBT_APMSUSPEND:
			log.Printf("[WndProc] SUSPEND")
		case win32.PBT_APMRESUMEAUTOMATIC:
			log.Printf("[WndProc] RESUME")
		}
	case w32.WM_SETTINGCHANGE:
		return 0
	case w32.WM_NCLBUTTONDOWN:
		w32.SetFocus(w.Handle())
	case w32.WM_MOVE, w32.WM_MOVING:
		w.Chromium.NotifyParentWindowPositionChanged()
	case 0x02E0: //w32.WM_DPICHANGED
		newWindowSize := (*w32.RECT)(unsafe.Pointer(lparam))
		w32.SetWindowPos(w.Handle(),
			uintptr(0),
			int(newWindowSize.Left),
			int(newWindowSize.Top),
			int(newWindowSize.Right-newWindowSize.Left),
			int(newWindowSize.Bottom-newWindowSize.Top),
			w32.SWP_NOZORDER|w32.SWP_NOACTIVATE)
	}

	if w.opts.Frameless {
		switch msg {
		case w32.WM_ACTIVATE:
			if w.opts.Frameless {
				win32.ExtendFrameIntoClientArea(w.Handle(), true)
			}
		case w32.WM_NCCALCSIZE:
			if wparam != 0 {
				rgrc := (*w32.RECT)(unsafe.Pointer(lparam))
				if w.Form.IsFullScreen() {
					w.Chromium.SetPadding(edge.Rect{})
				} else if w.IsMaximized() {
					monitor := w32.MonitorFromRect(rgrc, w32.MONITOR_DEFAULTTONULL)

					var monitorInfo w32.MONITORINFO
					monitorInfo.CbSize = uint32(unsafe.Sizeof(monitorInfo))
					if monitor != 0 && w32.GetMonitorInfo(monitor, &monitorInfo) {
						*rgrc = monitorInfo.RcWork

						maxWidth := w.opts.MaxWidth
						maxHeight := w.opts.MaxHeight
						if maxWidth > 0 || maxHeight > 0 {
							var dpiX, dpiY uint
							w32.GetDPIForMonitor(monitor, w32.MDT_EFFECTIVE_DPI, &dpiX, &dpiY)

							maxWidth := int32(winc.ScaleWithDPI(maxWidth, dpiX))
							if maxWidth > 0 && rgrc.Right-rgrc.Left > maxWidth {
								rgrc.Right = rgrc.Left + maxWidth
							}

							maxHeight := int32(winc.ScaleWithDPI(maxHeight, dpiY))
							if maxHeight > 0 && rgrc.Bottom-rgrc.Top > maxHeight {
								rgrc.Bottom = rgrc.Top + maxHeight
							}
						}
					}
					w.Chromium.SetPadding(edge.Rect{})
				} else {
					rgrc.Bottom += 1
					w.Chromium.SetPadding(edge.Rect{Bottom: 1})
				}
				return 0
			}
		}
	}
	return w.Form.WndProc(msg, wparam, lparam)
}

func (w *Window) IsMaximized() bool {
	return win32.IsWindowMaximized(w.Handle())
}

var edgeMap = map[string]uintptr{
	"top":          w32.HTTOP,
	"top-right":    w32.HTTOPRIGHT,
	"right":        w32.HTRIGHT,
	"bottom-right": w32.HTBOTTOMRIGHT,
	"bottom":       w32.HTBOTTOM,
	"bottom-left":  w32.HTBOTTOMLEFT,
	"left":         w32.HTLEFT,
	"top-left":     w32.HTTOPLEFT,
}

func (w *Window) StartResize(edge string) error {
	var border uintptr = edgeMap[edge]
	if !w32.ReleaseCapture() {
		return fmt.Errorf("unable to release mouse capture")
	}
	w32.PostMessage(w.Handle(), w32.WM_NCLBUTTONDOWN, border, 0)
	return nil
}
