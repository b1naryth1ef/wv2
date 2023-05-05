package wv2

import (
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
}

type Window struct {
	winc.Form

	opts     WindowOpts
	chromium *edge.Chromium
	handle   uintptr
}

func NewWindow(opts WindowOpts) *Window {
	chromium := edge.NewChromium()

	var exStyle = w32.WS_EX_CONTROLPARENT | w32.WS_EX_APPWINDOW
	var dwStyle = w32.WS_OVERLAPPEDWINDOW

	winc.RegClassOnlyOnce("wv2Window")
	handle := winc.CreateWindow("wv2Window", nil, uint(exStyle), uint(dwStyle))

	window := &Window{
		chromium: chromium,
		handle:   handle,
		opts:     opts,
	}

	window.SetIsForm(true)
	window.SetHandle(handle)
	winc.RegMsgHandler(window)

	win32.ShowWindow(handle)
	w32.SetForegroundWindow(handle)
	w32.SetFocus(handle)

	window.SetSize(opts.InitialWidth, opts.InitialHeight)

	chromium.AdditionalBrowserArgs = append(chromium.AdditionalBrowserArgs, "--enable-features=msWebView2EnableDraggableRegions")
	chromium.MessageCallback = window.processMessage
	chromium.WebResourceRequestedCallback = window.processRequest
	chromium.NavigationCompletedCallback = window.navigationCompleted

	chromium.Embed(handle)
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
			// If the window is frameless and we are minimizing, then we need to suppress the Resize on the
			// WebView2. If we don't do this, restoring does not work as expected and first restores with some wrong
			// size during the restore animation and only fully renders when the animation is done. This highly
			// depends on the content in the WebView, see https://github.com/wailsapp/wails/issues/1319
			event, _ := arg.Data.(*winc.SizeEventData)
			if event != nil && event.Type == w32.SIZE_MINIMIZED {
				return
			}
		}

		w.chromium.Resize()
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
		w.chromium.NotifyParentWindowPositionChanged()
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
			// If we want to have a frameless window but with the default frame decorations, extend the DWM client area.
			// This Option is not affected by returning 0 in WM_NCCALCSIZE.
			// As a result we have hidden the titlebar but still have the default window frame styling.
			// See: https://docs.microsoft.com/en-us/windows/win32/api/dwmapi/nf-dwmapi-dwmextendframeintoclientarea#remarks
			// if w.framelessWithDecorations {
			// 	win32.ExtendFrameIntoClientArea(w.Handle(), true)
			// }
		case w32.WM_NCCALCSIZE:
			// Disable the standard frame by allowing the client area to take the full
			// window size.
			// See: https://docs.microsoft.com/en-us/windows/win32/winmsg/wm-nccalcsize#remarks
			// This hides the titlebar and also disables the resizing from user interaction because the standard frame is not
			// shown. We still need the WS_THICKFRAME style to enable resizing from the frontend.
			if wparam != 0 {
				rgrc := (*w32.RECT)(unsafe.Pointer(lparam))
				if w.Form.IsFullScreen() {
					// In Full-Screen mode we don't need to adjust anything
					w.chromium.SetPadding(edge.Rect{})
				} else if w.IsMaximised() {
					// If the window is maximized we must adjust the client area to the work area of the monitor. Otherwise
					// some content goes beyond the visible part of the monitor.
					// Make sure to use the provided RECT to get the monitor, because during maximizig there might be
					// a wrong monitor returned in multi screen mode when using MonitorFromWindow.
					// See: https://github.com/MicrosoftEdge/WebView2Feedback/issues/2549
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
					w.chromium.SetPadding(edge.Rect{})
				} else {
					// This is needed to workaround the resize flickering in frameless mode with WindowDecorations
					// See: https://stackoverflow.com/a/6558508
					// The workaround originally suggests to decrese the bottom 1px, but that seems to bring up a thin
					// white line on some Windows-Versions, due to DrawBackground using also this reduces ClientSize.
					// Increasing the bottom also worksaround the flickering but we would loose 1px of the WebView content
					// therefore let's pad the content with 1px at the bottom.
					rgrc.Bottom += 1
					w.chromium.SetPadding(edge.Rect{Bottom: 1})
				}
				return 0
			}
		}
	}
	return w.Form.WndProc(msg, wparam, lparam)
}

func (w *Window) IsMaximised() bool {
	return win32.IsWindowMaximised(w.Handle())
}

func (w *Window) processMessage(message string) {
	log.Printf("processMessage(%v)", message)
}

func (w *Window) processRequest(req *edge.ICoreWebView2WebResourceRequest, args *edge.ICoreWebView2WebResourceRequestedEventArgs) {
	return
	// Setting the UserAgent on the CoreWebView2Settings clears the whole default UserAgent of the Edge browser, but
	// we want to just append our ApplicationIdentifier. So we adjust the UserAgent for every request.
	// if reqHeaders, err := req.GetHeaders(); err == nil {
	// 	useragent, _ := reqHeaders.GetHeader(assetserver.HeaderUserAgent)
	// 	useragent = strings.Join([]string{useragent, assetserver.WailsUserAgentValue}, " ")
	// 	reqHeaders.SetHeader(assetserver.HeaderUserAgent, useragent)
	// 	reqHeaders.Release()
	// }

	// if f.assets == nil {
	// 	// We are using the devServer let the WebView2 handle the request with its default handler
	// 	return
	// }

	// //Get the request
	// uri, _ := req.GetUri()
	// reqUri, err := url.ParseRequestURI(uri)
	// if err != nil {
	// 	f.logger.Error("Unable to parse equest uri %s: %s", uri, err)
	// 	return
	// }

	// if reqUri.Scheme != f.startURL.Scheme {
	// 	// Let the WebView2 handle the request with its default handler
	// 	return
	// } else if reqUri.Host != f.startURL.Host {
	// 	// Let the WebView2 handle the request with its default handler
	// 	return
	// }

	// rw := httptest.NewRecorder()
	// f.assets.ProcessHTTPRequestLegacy(rw, coreWebview2RequestToHttpRequest(req))

	// headers := []string{}
	// for k, v := range rw.Header() {
	// 	headers = append(headers, fmt.Sprintf("%s: %s", k, strings.Join(v, ",")))
	// }

	// env := f.chromium.Environment()
	// response, err := env.CreateWebResourceResponse(rw.Body.Bytes(), rw.Code, http.StatusText(rw.Code), strings.Join(headers, "\n"))
	// if err != nil {
	// 	f.logger.Error("CreateWebResourceResponse Error: %s", err)
	// 	return
	// }
	// defer response.Release()

	// // Send response back
	// err = args.PutResponse(response)
	// if err != nil {
	// 	f.logger.Error("PutResponse Error: %s", err)
	// 	return
	// }
}

func (w *Window) navigationCompleted(sender *edge.ICoreWebView2, args *edge.ICoreWebView2NavigationCompletedEventArgs) {
	log.Printf("navigationCopleted(%v, %v)", sender, args)
}
