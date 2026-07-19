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
	return parseSceneCardResponse(content, sceneID, pidSet, runID, model)
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
	return extractSceneCard(context.Background(), scene, paragraphs, prov, model, cfg, nil)
}
