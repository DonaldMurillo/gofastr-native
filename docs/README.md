# Documentation

Guides for building apps with gofastr-native. The terminal library has its
own guide in [`tui/README.md`](../tui/README.md).

## Start here

- [Getting started](getting-started.md): from an empty directory to a running app.
- [Developer workflow](developer-workflow.md): the CLI commands and what each one checks.
- [Running on a simulator, emulator or device](running-on-devices.md): what `run` does on each platform.
- [Android](android.md): native Android builds, from macOS, Linux or Windows.
- [Release builds](release.md): signing iOS and Android builds with your own credentials.
- [Debugging](debugging.md): traces and inspection for the `nativeui` runtime.

## Screens and behavior

- [One screen set, two renderers](shared-mobile-screens.md): how `nativeui` definitions reach UIKit, Android and the WebView.
- [Go-defined native presentation](native-presentation.md): writing screens in Go.
- [Runtime and built-in services](mobile-runtime.md)
- [Application lifecycle](mobile-lifecycle.md)
- [Presentation and selection behaviors](presentation-behaviors.md)
- [Editor behavior](editor-behavior.md): checkpoints and drafts with the `behavior` package.
- [Image previews](image-previews.md)
- [Component gallery](component-gallery.md): every screen contract, one at a time.

## Hosting a GoFastr web app

- [How a WebView request reaches Go](webview-hosting.md): the in-memory listener, the bridge and cookies.

## Device services

- [Device features](device-features.md): share-in, secure storage, widgets.
- [Files](files-services.md)
- [Photos](photos-services.md)
- [Secure storage](secure-storage.md)
- [Share-in](share-in.md)
- [Widgets](widgets.md)
- [iOS physical-device acceptance](ios-device-acceptance.md): what a simulator cannot check.

## Extensions

- [Rendering extensions](rendering-extensions.md): a library that draws part of a screen.
- [Compiled extensions](compiled-extensions.md): a library that adds a device adapter.
- [Extension tooling](extension-tooling.md): `extension init` and `extension check`.

## Design notes

[`design/`](design/) holds the [project status](design/status.md), the
roadmap, plans, cross-renderer contracts and the implementation log. They record why things are the way they are and are
not needed to build an app.
