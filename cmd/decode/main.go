package main

import (
	"fmt"
	"os"

	"dynamic-qris-go/qris"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: go run ./cmd/decode <image_path>")
		os.Exit(1)
	}

	text, err := qris.DecodeQRImage(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
	fmt.Println(text)
}
