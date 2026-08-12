package compiler

import (
	"fmt"
	"os"

	"github.com/nusapuksic/story/internal/store"
)

type compileArtifactCommitter struct {
	st              *store.Store
	staging         *RunStagingStore
	scenesFile      *os.File
	summariesFile   *os.File
	entitiesFile    *os.File
	occurrencesFile *os.File
}

func (c compileArtifactCommitter) CommitScenes(output sceneWorkOutput) error {
	if c.st == nil {
		return fmt.Errorf("commit scenes: store is nil")
	}
	if c.scenesFile == nil {
		return fmt.Errorf("commit scenes: scenes file is nil")
	}
	input := output.Input
	for _, sc := range output.Scenes {
		if err := appendJSONL(c.scenesFile, sc); err != nil {
			return err
		}
	}
	if err := appendJSONL(c.scenesFile, output.Snapshot); err != nil {
		return fmt.Errorf("write chapter_snapshot for %s: %w", input.Chapter.ID, err)
	}

	if err := c.st.DeleteScenesForChapter(input.Chapter.ID); err != nil {
		return err
	}
	for _, sc := range output.Scenes {
		if err := c.st.InsertScene(sceneRowFromRecord(sc)); err != nil {
			return err
		}
	}
	if err := c.st.MarkChapterSnapshotCommitted(input.Chapter.ID, output.Snapshot.CommittedAt); err != nil {
		return err
	}
	return c.recordCommit(output.Staged)
}

func (c compileArtifactCommitter) CommitSceneCard(output sceneCardWorkOutput) error {
	if c.st == nil {
		return fmt.Errorf("commit scene card: store is nil")
	}
	if c.scenesFile == nil {
		return fmt.Errorf("commit scene card: scenes file is nil")
	}
	if output.Card == nil {
		return fmt.Errorf("commit scene card: card is nil")
	}
	if err := appendJSONL(c.scenesFile, output.Card); err != nil {
		return err
	}
	if output.Card.Status == SceneCardStatusSkipped {
		if err := c.st.DeleteSceneCard(output.Input.Scene.ID); err != nil {
			return err
		}
		return c.recordCommit(output.Staged)
	}
	if err := c.st.InsertSceneCard(sceneCardRowFromRecord(*output.Card)); err != nil {
		return err
	}
	return c.recordCommit(output.Staged)
}

func (c compileArtifactCommitter) CommitVerification(output verificationWorkOutput) error {
	if c.st == nil {
		return fmt.Errorf("commit verification: store is nil")
	}
	if c.scenesFile == nil {
		return fmt.Errorf("commit verification: scenes file is nil")
	}
	if err := appendJSONL(c.scenesFile, output.Updated); err != nil {
		return err
	}
	if err := c.st.InsertSceneCard(sceneCardRowFromRecord(output.Updated)); err != nil {
		return err
	}
	return c.recordCommit(output.Staged)
}

func (c compileArtifactCommitter) CommitSummary(output summaryWorkOutput) error {
	if c.st == nil {
		return fmt.Errorf("commit summary: store is nil")
	}
	if c.summariesFile == nil {
		return fmt.Errorf("commit summary: summaries file is nil")
	}
	if output.Record == nil {
		return fmt.Errorf("commit summary: record is nil")
	}
	if err := appendJSONL(c.summariesFile, output.Record); err != nil {
		return err
	}
	if err := c.st.InsertSummary(summaryRowFromRecord(*output.Record)); err != nil {
		return err
	}
	return c.recordCommit(output.Staged)
}

func (c compileArtifactCommitter) CommitBookSummary(ref StagedResultRef, record *SummaryRecord) error {
	if c.st == nil {
		return fmt.Errorf("commit book summary: store is nil")
	}
	if c.summariesFile == nil {
		return fmt.Errorf("commit book summary: summaries file is nil")
	}
	if record == nil {
		return fmt.Errorf("commit book summary: record is nil")
	}
	if err := appendJSONL(c.summariesFile, record); err != nil {
		return err
	}
	if err := c.st.InsertSummary(summaryRowFromRecord(*record)); err != nil {
		return err
	}
	return c.recordCommit(ref)
}

func (c compileArtifactCommitter) CommitEntities(output entityWorkOutput, entities []EntityRecord, occurrences []OccurrenceRecord) error {
	if c.st == nil {
		return fmt.Errorf("commit entities: store is nil")
	}
	if c.entitiesFile == nil {
		return fmt.Errorf("commit entities: entities file is nil")
	}
	if c.occurrencesFile == nil {
		return fmt.Errorf("commit entities: occurrences file is nil")
	}
	entityRecords := make([]any, 0, len(entities)+1)
	entityRows := make([]store.EntityRow, 0, len(entities))
	for _, entity := range entities {
		entityRecords = append(entityRecords, entity)
		entityRows = append(entityRows, entityRowFromRecord(entity))
	}
	entityRecords = append(entityRecords, output.Snapshot)
	if err := appendJSONLBatch(c.entitiesFile, entityRecords); err != nil {
		return fmt.Errorf("write entity_snapshot for %s: %w", output.Input.Chapter.ID, err)
	}

	occurrenceRecords := make([]any, 0, len(occurrences))
	occurrenceRows := make([]store.OccurrenceRow, 0, len(occurrences))
	for _, occurrence := range occurrences {
		occurrenceRecords = append(occurrenceRecords, occurrence)
		occurrenceRows = append(occurrenceRows, occurrenceRowFromRecord(occurrence))
	}
	if err := appendJSONLBatch(c.occurrencesFile, occurrenceRecords); err != nil {
		return err
	}

	if err := c.st.ReplaceEntityProjectionForChapter(
		output.Input.Chapter.ID,
		entityRows,
		occurrenceRows,
		output.Snapshot.EntityCount,
		output.Snapshot.OccurrenceCount,
		output.Snapshot.CommittedAt,
	); err != nil {
		return err
	}
	return c.recordCommit(output.Staged)
}

func (c compileArtifactCommitter) recordCommit(ref StagedResultRef) error {
	if c.staging == nil {
		return nil
	}
	return c.staging.RecordCommit(ref)
}
