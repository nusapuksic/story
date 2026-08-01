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
	if c.summariesFile == nil {
		return fmt.Errorf("commit summary: summaries file is nil")
	}
	if output.Record == nil {
		return fmt.Errorf("commit summary: record is nil")
	}
	if err := appendJSONL(c.summariesFile, output.Record); err != nil {
		return err
	}
	return c.recordCommit(output.Staged)
}

func (c compileArtifactCommitter) CommitBookSummary(ref StagedResultRef, record *SummaryRecord) error {
	if c.summariesFile == nil {
		return fmt.Errorf("commit book summary: summaries file is nil")
	}
	if record == nil {
		return fmt.Errorf("commit book summary: record is nil")
	}
	if err := appendJSONL(c.summariesFile, record); err != nil {
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
	for _, entity := range entities {
		if err := appendJSONL(c.entitiesFile, entity); err != nil {
			return err
		}
	}
	for _, occurrence := range occurrences {
		if err := appendJSONL(c.occurrencesFile, occurrence); err != nil {
			return err
		}
	}
	if err := appendJSONL(c.entitiesFile, output.Snapshot); err != nil {
		return fmt.Errorf("write entity_snapshot for %s: %w", output.Input.Chapter.ID, err)
	}
	if err := c.st.DeleteEntityOccurrencesForChapter(output.Input.Chapter.ID); err != nil {
		return err
	}
	for _, entity := range entities {
		if err := c.st.InsertEntity(entityRowFromRecord(entity)); err != nil {
			return err
		}
	}
	for _, occurrence := range occurrences {
		if err := c.st.InsertOccurrence(occurrenceRowFromRecord(occurrence)); err != nil {
			return err
		}
	}
	if err := c.st.MarkEntitySnapshotCommitted(output.Input.Chapter.ID, output.Snapshot.EntityCount, output.Snapshot.OccurrenceCount, output.Snapshot.CommittedAt); err != nil {
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
