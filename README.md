# wv2

wv2 provides Go bindings to the [WebView2 runtime](https://developer.microsoft.com/en-us/microsoft-edge/webview2/) based off the prior work in [wails](https://github.com/wailsapp/wails).

## example

```go
package main

import "github.com/b1naryth1ef/wv2"

func main() {
	window := wv2.NewWindow(wv2.WindowOpts{
		Frameless:  true,
		InitialURL: "https://google.com",
	})
	window.Run()
}
```