// Package fcm bundles the Firebase Cloud Messaging push provider. It is
// compiled only into builds that enable push and link the Firebase libraries.
package fcm

import "embed"

//go:embed *.java
var Sources embed.FS
