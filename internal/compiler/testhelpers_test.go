package compiler

import (
	"context"

	"github.com/nusapuksic/story/internal/provider"
	"github.com/nusapuksic/story/internal/store"
)

// ParseSceneCardResponseForTest is an exported wrapper for tests.
func ParseSceneCardResponseForTest(
	content, sceneID string,
	pidSet map[string]bool,
	runID, model string,
) (*SceneCardRecord, error) {
	return parseSceneCardResponse(content, sceneID, pidSet, nil, runID, model, "scene-extraction-v1")
}

// ExtractSceneCardForTest exercises extractSceneCard with a real provider.
func ExtractSceneCardForTest(
	prov provider.Provider,
	scene store.SceneRow,
	paragraphs []store.ParagraphRow,
	model string,
) (*SceneCardRecord, error) {
	cfg := sceneDetectConfig{
		Mode:            "explicit",
		MaxOutputTokens: 3000,
		Temperature:     0.1,
	}
	return extractSceneCard(context.Background(), nil, scene, paragraphs, prov, model, cfg, nil, SceneCardFailurePolicyRetryFallback)
}

// ExtractSceneCardStrictForTest exercises extractSceneCard with strict failure behavior.
func ExtractSceneCardStrictForTest(
	prov provider.Provider,
	scene store.SceneRow,
	paragraphs []store.ParagraphRow,
	model string,
) (*SceneCardRecord, error) {
	cfg := sceneDetectConfig{
		Mode:            "explicit",
		MaxOutputTokens: 3000,
		Temperature:     0.1,
	}
	return extractSceneCard(context.Background(), nil, scene, paragraphs, prov, model, cfg, nil, SceneCardFailurePolicyStrict)
}
