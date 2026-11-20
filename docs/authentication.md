# Sign in with Apple and OAuth on iOS

The native UIKit renderer supports Sign in with Apple via
`ASAuthorizationController` and browser-based OAuth authorization code flows
via `ASWebAuthenticationSession`. Choose the operation with
`device/authentication.Request` in a `nativeui.ServiceRequest.Authentication`.
The renderer advertises `CapabilitySignInWithApple` and
`CapabilityOAuthWebLogin` separately. Android and WebView do not run these
operations.

```go
return nativeui.Result{Service: &nativeui.ServiceRequest{
    ID: "sign-in",
    Authentication: &authentication.Request{
        Operation: authentication.Apple,
        Nonce:     appleNonceForThisAttempt,
    },
}}, nil
```

Register a `Route.Services["sign-in"]` continuation to consume the outcome.
`cancelled`, `failed` and `unsupported` never carry credentials. On
`completed`, Apple returns an authorization code, identity token and Apple
user identifier; validate the token and nonce on your backend, with the
expected issuer, audience and expiry, before establishing an app session.
Generate a fresh nonce for each attempt. This API sends the nonce supplied
by your app as-is. It does not request name or email scopes.

For OAuth, supply `Operation: authentication.OAuth`, an HTTPS
`AuthorizationURL`, a custom-scheme `RedirectURI` (for example
`com.example.myapp:/oauth`), and a fresh unpredictable `State`. Build the URL
with the provider's client ID, exact redirect URI, `response_type=code`, the
same state, and an S256 PKCE challenge. Keep the original PKCE verifier for
the exchange. The adapter validates the callback scheme, host, path and
state, and returns only the authorization code and state. It does not
exchange tokens, support implicit flows or HTTPS callbacks, or assume a
provider-specific login endpoint. Set `PreferEphemeralSession` to request a
browser session without sharing the user's normal browser cookies.

Treat these results as sensitive. Do not put credentials in screen state,
announcements, routes or logs. A completed service means the OS returned
authorization material, not that the user is signed in to your app.

## Developer account and provider setup

- Enable **Sign in with Apple** on the app's explicit App ID in the
  [Apple Developer portal](https://developer.apple.com/account/resources/identifiers/list),
  regenerate the provisioning profile, and build the native iOS app with
  `--sign-in-with-apple`. The device signer rejects profiles without
  `com.apple.developer.applesignin = [Default]`. With your own native host,
  add the capability in Xcode and set `GofastrSignInWithApple` to `true` in
  the app's Info.plist.
- Register the public native client and exact callback URI with the OAuth
  provider, and use an application-specific reverse-DNS scheme. The system
  session handles the OAuth callback; do not forward it as an app navigation
  route. Never bundle a confidential OAuth client secret.
- Verify successful Apple authorization on a provisioned iPhone and
  successful OAuth sign-in against the configured provider before release.

## Simulator demo

The shared device-features example has a **Native sign-in** screen. It
uses a reserved example domain rather than an OAuth provider. For ad hoc
simulator builds, `--sign-in-with-apple` enables the Apple API call but
does not add the restricted device entitlement; expect failure or
cancellation, not a successful login. Do not enter account credentials in
the demo.
On an English-language simulator without an Apple Account:

```sh
GOWORK=off go run ./cmd/gofastr-native build --platform ios-sim --renderer native \
  --pkg ./examples/devicefeatures/featuresnative --sign-in-with-apple \
  --id dev.gofastr.features --url-scheme dev.gofastr.features --out dist/auth-ios
xcrun simctl install "$SIMULATOR_UDID" dist/auth-ios/Featuresnative.app
xcodebuild test -project examples/devicefeatures/ios-ui-tests/AuthUITests.xcodeproj \
  -scheme AuthUITests -destination "platform=iOS Simulator,id=$SIMULATOR_UDID" \
  -parallel-testing-enabled NO
ditto -c -k --keepParent dist/auth-ios/Featuresnative.app dist/auth-ios/Featuresnative-simulator.zip
```

Unzip the simulator artifact and install its `.app` with `xcrun simctl
install`. The build targets the simulator host's processor architecture
and is not an iPhone-installable distribution build.
