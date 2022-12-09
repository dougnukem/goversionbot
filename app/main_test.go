package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestGoVersionMessage test GoVersionMessage.
func TestGoVersionMessageMajor(t *testing.T) {
	version := "go1.19.0"
	msg := getGoVersionMessage(version)

	assert.Equal(t, `A new Go version [go1.19.0] is available, download for MacOS here: https://go.dev/dl/gov1.19.darwin-amd64.pkg <Release Notes|https://go.dev/doc/devel/release#gov1.19> <Github Milestone|https://github.com/golang/go/issues?q=milestone%3AGov1.19>`, msg, "Expected go version message for major to match")
}

func TestGoVersionMessageMinorPatch(t *testing.T) {
	version := "go1.19.4"
	msg := getGoVersionMessage(version)

	assert.Equal(t, `A new Go version [go1.19.4] is available, download for MacOS here: https://go.dev/dl/go1.19.4.darwin-amd64.pkg <Release Notes|https://go.dev/doc/devel/release#gov1.19.minor> <Github Milestone|https://github.com/golang/go/issues?q=milestone%3AGogo1.19.4+label%3ACherryPickApproved+>`, msg, "Expected go version message for minor patch version to match")
}
