package main

import (
	"path/filepath"
	"strings"

	"github.com/nusapuksic/story/internal/compiler"
	"github.com/nusapuksic/story/internal/project"
	"github.com/nusapuksic/story/internal/query"
)

func characterRoleContextForAsk(p *project.Project) ([]query.CharacterRoleContext, error) {
	records, _, err := compiler.ReadLatestCharacterRoles(p.Path(filepath.Join(project.ModelDir, "character_roles.jsonl")))
	if err != nil {
		return nil, err
	}
	out := make([]query.CharacterRoleContext, 0, len(records))
	for _, rec := range records {
		if strings.TrimSpace(rec.CharacterID) == "" || strings.TrimSpace(rec.CanonicalName) == "" {
			continue
		}
		out = append(out, query.CharacterRoleContext{
			RecordType:      rec.RecordType,
			CharacterID:     rec.CharacterID,
			SourceEntityIDs: copyStrings(rec.SourceEntityIDs),
			CanonicalName:   rec.CanonicalName,
			Aliases:         copyStrings(rec.Aliases),
			Classification:  rec.Classification,
			Role:            rec.Role,
			Confidence:      rec.Confidence,
			Rationale:       rec.Rationale,
			Evidence:        characterRoleEvidenceForAsk(rec.Evidence),
			Status:          rec.Status,
		})
	}
	return out, nil
}

func characterRoleEvidenceForAsk(values []compiler.CharacterRoleEvidence) []query.CharacterRoleEvidence {
	if len(values) == 0 {
		return nil
	}
	out := make([]query.CharacterRoleEvidence, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value.SceneID) == "" {
			continue
		}
		out = append(out, query.CharacterRoleEvidence{
			SceneID: value.SceneID,
			Reason:  value.Reason,
		})
	}
	return out
}
