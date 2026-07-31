package main

import (
	"testing"

	"github.com/nusapuksic/story/internal/compiler"
)

func TestCompileNeedsVerificationProviderUsesMode(t *testing.T) {
	if compileNeedsVerificationProvider("", compiler.VerificationModeOff) {
		t.Fatal("full compile with verification off should not require verification provider")
	}
	if !compileNeedsVerificationProvider("", compiler.VerificationModeRecovered) {
		t.Fatal("full compile with recovered verification mode should require verification provider")
	}
	if !compileNeedsVerificationProvider(compiler.LayerVerification, compiler.VerificationModeOff) {
		t.Fatal("explicit verification layer should require verification provider")
	}
}
