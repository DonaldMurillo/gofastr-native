// The chord harp's phone app. It is its own module because it shares the
// synthesizer with the terminal app in the tui module, which the root module
// does not depend on.
module github.com/DonaldMurillo/gofastr-native/examples/chordharp

go 1.27.3

require (
	github.com/DonaldMurillo/gofastr-native v0.0.0
	github.com/DonaldMurillo/gofastr-native/tui v0.0.0
	golang.org/x/sys v0.49.0
)

require (
	github.com/DonaldMurillo/gofastr v0.96.0 // indirect
	golang.org/x/image v0.47.0 // indirect
	golang.org/x/mobile v0.0.0-20261011034528-197770444867 // indirect
	golang.org/x/mod v0.42.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/text v0.43.0 // indirect
	golang.org/x/tools v0.52.0 // indirect
)

replace (
	github.com/DonaldMurillo/gofastr-native => ../..
	github.com/DonaldMurillo/gofastr-native/tui => ../../tui
)

tool (
	golang.org/x/mobile/cmd/gobind
	golang.org/x/mobile/cmd/gomobile
)
