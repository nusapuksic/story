package compiler

import (
	"runtime"
	"testing"

	"github.com/nusapuksic/story/internal/provider"
)

type sceneWorkerLimitProvider struct {
	provider.Provider
}

func TestSceneDetectionWorkerLimitUsesNumCPUForExplicitMode(t *testing.T) {
	got := sceneDetectionWorkerLimit(sceneDetectConfig{Mode: " explicit "}, Options{ExtractionProvider: sceneWorkerLimitProvider{}})
	if got != runtime.NumCPU() {
		t.Fatalf("sceneDetectionWorkerLimit explicit = %d, want runtime.NumCPU() %d", got, runtime.NumCPU())
	}
}

func TestSceneDetectionWorkerLimitStaysSerialForModelAssistedMode(t *testing.T) {
	got := sceneDetectionWorkerLimit(sceneDetectConfig{Mode: "hybrid"}, Options{ExtractionProvider: sceneWorkerLimitProvider{}})
	if got != 1 {
		t.Fatalf("sceneDetectionWorkerLimit hybrid with provider = %d, want 1", got)
	}
}

func TestSceneDetectionWorkerLimitUsesNumCPUWithoutProvider(t *testing.T) {
	got := sceneDetectionWorkerLimit(sceneDetectConfig{Mode: "hybrid"}, Options{})
	if got != runtime.NumCPU() {
		t.Fatalf("sceneDetectionWorkerLimit no provider = %d, want runtime.NumCPU() %d", got, runtime.NumCPU())
	}
}
