//go:build !windows

package main

import (
	"context"
	"log"

	"github.com/DonaldMurillo/gofastr-native/render/desktop"
)

func main() {
	if err := desktop.Run(context.Background(), nil, nil); err != nil {
		log.Fatal(err)
	}
}
