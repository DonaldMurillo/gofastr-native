# How a WebView request reaches Go

This applies to the WebView host, which runs a whole GoFastr web app inside
the phone app's process.

- **Boot-time GETs** (the first document, stylesheets, scripts, images) go
  through the platform's interception API: `WKURLSchemeHandler` on iOS,
  `shouldInterceptRequest` on Android. The shell hands each one to `Handle`
  or `Open`.
- **Runtime requests** (client navigation, RPCs, form saves, islands) go
  through `window.__gofastr_transport`, which `bridge.js` installs. It sends
  the method, URL, headers and base64 body as JSON over the native channel to
  `Host.Bridge`:
  - iOS: a `WKScriptMessageHandlerWithReply` named `gofastr`, main frame and
    app scheme only.
  - Android: a `WebMessageChannel` port posted to the app origin after each
    page load, instead of `addJavascriptInterface`.
- **Native POST forms** the runtime leaves alone are sent the same way with
  `navigate: true`. The shell shows the answer as the next document:
  `loadSimulatedRequest` on iOS, `loadDataWithBaseURL` on Android.
- **Redirects** are followed in Go (the Fetch Standard's limit of 20), and
  the reply carries the final URL and a `redirected` flag, which the runtime
  reads.
- **Cookies** stay in a Go cookie jar. `Set-Cookie` never reaches the
  WebView.

The bridge buffers: a bridged request returns its whole body at once, capped
at 64 MiB. A long-lived stream (SSE) or a large download goes through the
interception path and `Open`, which hands chunks over as they arrive.

