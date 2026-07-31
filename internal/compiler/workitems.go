package compiler

import "github.com/nusapuksic/story/internal/store"

type sceneCardWorkInput struct {
	ChapterID             string
	ChapterIndex          int
	ChapterTotal          int
	SceneIndex            int
	SceneTotal            int
	Scene                 store.SceneRow
	Paragraphs            []store.ParagraphRow
	SkipRecoveryOnFailure bool
	PromptTokens          int
}

type sceneCardWorkOutput struct {
	Input    sceneCardWorkInput
	Card     *SceneCardRecord
	Recovery *SceneCardRecoveryEvent
	Progress []ProgressEvent
	Staged   StagedResultRef
}

type stagedSceneCardPayload struct {
	Card     *SceneCardRecord        `json:"card"`
	Recovery *SceneCardRecoveryEvent `json:"recovery,omitempty"`
	Skipped  bool                    `json:"skipped,omitempty"`
}

type verificationWorkInput struct {
	ChapterID          string
	ChapterIndex       int
	ChapterTotal       int
	SceneIndex         int
	SceneTotal         int
	Scene              store.SceneRow
	Card               SceneCardRecord
	EvidenceParagraphs []store.ParagraphRow
}

type verificationWorkOutput struct {
	Input        verificationWorkInput
	Verification SceneCardVerification
	Updated      SceneCardRecord
	Staged       StagedResultRef
}

type stagedVerificationPayload struct {
	Card         SceneCardRecord       `json:"card"`
	Verification SceneCardVerification `json:"verification"`
}
