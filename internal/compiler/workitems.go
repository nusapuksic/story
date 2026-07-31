package compiler

import "github.com/nusapuksic/story/internal/store"

type sceneWorkInput struct {
	Chapter       store.ChapterRow
	ChapterIndex  int
	ChapterTotal  int
	Paragraphs    []store.ParagraphRow
	BreakOrdinals []int
}

type sceneWorkOutput struct {
	Input    sceneWorkInput
	Scenes   []SceneRecord
	Snapshot ChapterSnapshotRecord
	Staged   StagedResultRef
}

type stagedScenesPayload struct {
	Scenes   []SceneRecord         `json:"scenes"`
	Snapshot ChapterSnapshotRecord `json:"snapshot"`
}

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

type entityWorkInput struct {
	Chapter       store.ChapterRow
	ChapterIndex  int
	ChapterTotal  int
	Paragraphs    []store.ParagraphRow
	PromptContext entityExtractionContext
	Force         bool
}

type entityWorkOutput struct {
	Input      entityWorkInput
	Candidates []entityRecordCandidate
	Staged     StagedResultRef
}

type stagedEntityPayload struct {
	Candidates []entityRecordCandidate `json:"candidates"`
}

type entityRecordCandidate struct {
	Type          string                   `json:"type"`
	CanonicalName string                   `json:"canonical_name"`
	Aliases       []string                 `json:"aliases,omitempty"`
	Evidence      []string                 `json:"evidence"`
	Mentions      []mentionRecordCandidate `json:"mentions"`
	Generation    EntityGeneration         `json:"generation"`
	Status        string                   `json:"status"`
}

type mentionRecordCandidate struct {
	ChapterID   string           `json:"chapter_id"`
	ParagraphID string           `json:"paragraph_id"`
	SurfaceText string           `json:"surface_text"`
	Confidence  float64          `json:"confidence"`
	Generation  EntityGeneration `json:"generation"`
	Status      string           `json:"status"`
}
type summaryWorkInput struct {
	Chapter      store.ChapterRow
	ChapterIndex int
	ChapterTotal int
	Paragraphs   []store.ParagraphRow
}

type summaryWorkOutput struct {
	Input  summaryWorkInput
	Record *SummaryRecord
	Staged StagedResultRef
}

type stagedSummaryPayload struct {
	Record *SummaryRecord `json:"record"`
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
