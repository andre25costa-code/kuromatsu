//go:build !linux

package main

import "fmt"

func readRSS() (uint64, error) {
	return 0, fmt.Errorf("RSS reporting is only implemented on linux (the deploy target)")
}
