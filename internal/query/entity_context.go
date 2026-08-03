package query

import (
	"sort"
	"strings"

	"github.com/nusapuksic/story/internal/store"
)

const (
	defaultEntityContextLimit    = 6
	defaultEntityOccurrenceLimit = 8
	defaultEntityListValueLimit  = 6
)

// EntityContext is compiled entity context made available to ask prompts.
type EntityContext struct {
	Entity      store.EntityRow
	Occurrences []store.OccurrenceRow
}

type scoredEntityContext struct {
	ctx   EntityContext
	score int
	index int
}

func entityContextForQuestion(st *store.Store, question, mode, chapterID string) ([]EntityContext, error) {
	entities, err := st.EntityRowsForChapter(chapterID)
	if err != nil {
		return nil, err
	}
	if len(entities) == 0 {
		return nil, nil
	}

	normalizedQuestion := normalizeQuestionText(question)
	wantsEntityContext := isCharacterQuestion(normalizedQuestion) || characterMode(mode)
	var scored []scoredEntityContext
	for i, entity := range entities {
		score := entityQuestionScore(normalizedQuestion, entity, wantsEntityContext)
		if score == 0 {
			continue
		}
		occurrences, err := st.OccurrencesForEntity(entity.ID, chapterID, defaultEntityOccurrenceLimit)
		if err != nil {
			return nil, err
		}
		scored = append(scored, scoredEntityContext{
			ctx: EntityContext{
				Entity:      entity,
				Occurrences: occurrences,
			},
			score: score,
			index: i,
		})
	}
	if len(scored) == 0 {
		return nil, nil
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		left := strings.ToLower(scored[i].ctx.Entity.CanonicalName)
		right := strings.ToLower(scored[j].ctx.Entity.CanonicalName)
		if left != right {
			return left < right
		}
		return scored[i].index < scored[j].index
	})
	if len(scored) > defaultEntityContextLimit {
		scored = scored[:defaultEntityContextLimit]
	}

	out := make([]EntityContext, len(scored))
	for i, item := range scored {
		out[i] = item.ctx
	}
	return out, nil
}

func entityQuestionScore(normalizedQuestion string, entity store.EntityRow, wantsEntityContext bool) int {
	score := 0
	if entityNameMentioned(normalizedQuestion, entity.CanonicalName) {
		score += 100
	}
	for _, alias := range entity.Aliases {
		if entityNameMentioned(normalizedQuestion, alias) {
			score += 80
			break
		}
	}

	entityType := strings.TrimSpace(strings.ToLower(entity.Type))
	if wantsEntityContext {
		switch entityType {
		case "character":
			score += 20
		case "group", "organization":
			score += 8
		case "unknown":
			score += 4
		}
	}
	return score
}

func entityNameMentioned(normalizedQuestion, value string) bool {
	value = normalizeQuestionText(value)
	if value == "" {
		return false
	}
	return strings.Contains(" "+normalizedQuestion+" ", " "+value+" ")
}

func characterMode(mode string) bool {
	switch strings.TrimSpace(strings.ToLower(mode)) {
	case "continuity", "development":
		return true
	default:
		return false
	}
}

func isCharacterQuestion(normalizedQuestion string) bool {
	for _, phrase := range []string{
		"character",
		"characters",
		"protagonist",
		"antagonist",
		"narrator",
		"pov",
		"point of view",
		"who is",
		"who are",
		"relationship",
		"relationships",
		"know",
		"knows",
		"believe",
		"believes",
		"want",
		"wants",
		"motivation",
		"motivations",
		"arc",
		"arcs",
	} {
		if containsQuestionPhrase(normalizedQuestion, phrase) {
			return true
		}
	}
	return false
}
