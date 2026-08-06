package query

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/nusapuksic/story/internal/provider"
	"github.com/nusapuksic/story/internal/retrieval"
	"github.com/nusapuksic/story/internal/store"
)

type queryIntent string

const (
	intentNarrowRecall       queryIntent = "narrow_recall"
	intentStorySummary       queryIntent = "story_summary"
	intentChapterSummary     queryIntent = "chapter_summary"
	intentCharacterInventory queryIntent = "character_inventory"
	intentCharacterQuestion  queryIntent = "character_question"
	intentCharacterArc       queryIntent = "character_arc"
	intentThemeStyle         queryIntent = "theme_style"
	intentEnding             queryIntent = "ending"
	intentBroad              queryIntent = "broad"
)

const (
	coverageSceneCardLimit             = 12
	defaultCharacterRoleContextLimit   = 8
	inventoryCharacterRoleContextLimit = 24
)

type evidencePacket struct {
	Intent         queryIntent
	Summaries      []SummaryContext
	CharacterRoles []CharacterRoleContext
	EntityContext  []EntityContext
	Digests        []EvidenceDigest
	SceneCards     []store.SceneCardRow
	Paragraphs     []store.ParagraphRow
	RecordsUsed    []string
}

func classifyQueryIntent(question, mode, chapterID string) queryIntent {
	normalized := normalizeQuestionText(question)
	if isEndingQuestion(question) {
		return intentEnding
	}
	if isCharacterInventoryQuestion(normalized) {
		return intentCharacterInventory
	}
	if isCharacterArcQuestion(normalized) {
		return intentCharacterArc
	}
	if isSummaryQuestion(normalized) {
		if strings.TrimSpace(chapterID) != "" || containsQuestionPhrase(normalized, "chapter") || containsQuestionPhrase(normalized, "chapter by chapter") {
			return intentChapterSummary
		}
		return intentStorySummary
	}
	if isThemeStyleQuestion(normalized, mode) {
		return intentThemeStyle
	}
	if isBroadStructuralQuestion(normalized) {
		return intentBroad
	}
	if isCharacterQuestion(normalized) || characterMode(mode) {
		return intentCharacterQuestion
	}
	return intentNarrowRecall
}

func retrievalParagraphLimit(maxEvidence int, intent queryIntent) int {
	if maxEvidence <= 0 {
		maxEvidence = 20
	}
	if !intentUsesBroadCoverage(intent) {
		return maxEvidence
	}
	limit := maxEvidence * 4
	if limit < maxEvidence+20 {
		limit = maxEvidence + 20
	}
	return limit
}

func buildEvidencePacket(
	ctx context.Context,
	st *store.Store,
	prov provider.Provider,
	model string,
	question string,
	opts Options,
	intent queryIntent,
	ret retrieval.Result,
	paragraphs []store.ParagraphRow,
	entityContext []EntityContext,
	usedParagraphFallback bool,
	cardPolicy store.SceneCardStatusPolicy,
) (evidencePacket, error) {
	summaries := summariesForIntent(opts.Summaries, intent, opts.ChapterID)
	roles := characterRolesForIntent(opts.CharacterRoles, entityContext, question, intent, opts.Mode)
	cards := ret.SceneCards
	if shouldUseCoverageSceneCards(intent, summaries, cards) {
		allCards, err := st.AllSceneCardsByStatusPolicyForChapter(opts.ChapterID, cardPolicy)
		if err != nil {
			return evidencePacket{}, fmt.Errorf("coverage scene-card retrieval: %w", err)
		}
		cards = mergeSceneCards(cards, selectSceneCardsForIntent(intent, allCards))
	}

	digests, err := maybeCondenseParagraphs(ctx, prov, model, question, opts, intent, summaries, cards, paragraphs)
	if err != nil {
		return evidencePacket{}, err
	}
	paragraphs = packParagraphs(paragraphs, opts.MaxEvidence, intent, usedParagraphFallback)

	packet := evidencePacket{
		Intent:         intent,
		Summaries:      summaries,
		CharacterRoles: roles,
		EntityContext:  entityContext,
		Digests:        digests,
		SceneCards:     cards,
		Paragraphs:     paragraphs,
	}
	packet.RecordsUsed = packet.defaultRecordsUsed()
	return packet, nil
}

func maybeCondenseParagraphs(
	ctx context.Context,
	prov provider.Provider,
	model string,
	question string,
	opts Options,
	intent queryIntent,
	summaries []SummaryContext,
	cards []store.SceneCardRow,
	paragraphs []store.ParagraphRow,
) ([]EvidenceDigest, error) {
	if len(paragraphs) <= opts.MaxEvidence || !intentUsesBroadCoverage(intent) {
		return nil, nil
	}
	if hasVisibleSummaryContext(summaries) || len(cards) > 0 {
		return nil, nil
	}
	digests, err := condenseParagraphEvidence(ctx, prov, model, question, opts, paragraphs)
	if err != nil {
		return nil, fmt.Errorf("condense evidence: %w", err)
	}
	return digests, nil
}

func packParagraphs(paragraphs []store.ParagraphRow, maxEvidence int, intent queryIntent, usedParagraphFallback bool) []store.ParagraphRow {
	if maxEvidence <= 0 {
		maxEvidence = 20
	}
	if len(paragraphs) <= maxEvidence {
		return paragraphs
	}
	if intent == intentEnding && usedParagraphFallback {
		return paragraphs[len(paragraphs)-maxEvidence:]
	}
	if intentUsesBroadCoverage(intent) || usedParagraphFallback {
		return spreadParagraphs(paragraphs, maxEvidence)
	}
	return paragraphs[:maxEvidence]
}

func spreadParagraphs(paragraphs []store.ParagraphRow, limit int) []store.ParagraphRow {
	if limit <= 0 || len(paragraphs) <= limit {
		return paragraphs
	}
	if limit == 1 {
		return []store.ParagraphRow{paragraphs[0]}
	}
	out := make([]store.ParagraphRow, 0, limit)
	seen := make(map[int]bool, limit)
	for i := 0; i < limit; i++ {
		idx := int(math.Round(float64(i) * float64(len(paragraphs)-1) / float64(limit-1)))
		if seen[idx] {
			continue
		}
		seen[idx] = true
		out = append(out, paragraphs[idx])
	}
	return out
}

func selectSceneCardsForIntent(intent queryIntent, cards []store.SceneCardRow) []store.SceneCardRow {
	if intent == intentEnding {
		return tailSceneCards(cards, endingFallbackSceneCardLimit)
	}
	return spreadSceneCards(cards, coverageSceneCardLimit)
}

func spreadSceneCards(cards []store.SceneCardRow, limit int) []store.SceneCardRow {
	if limit <= 0 || len(cards) <= limit {
		return cards
	}
	if limit == 1 {
		return []store.SceneCardRow{cards[0]}
	}
	out := make([]store.SceneCardRow, 0, limit)
	seen := make(map[int]bool, limit)
	for i := 0; i < limit; i++ {
		idx := int(math.Round(float64(i) * float64(len(cards)-1) / float64(limit-1)))
		if seen[idx] {
			continue
		}
		seen[idx] = true
		out = append(out, cards[idx])
	}
	return out
}

func mergeSceneCards(left, right []store.SceneCardRow) []store.SceneCardRow {
	if len(right) == 0 {
		return left
	}
	out := make([]store.SceneCardRow, 0, len(left)+len(right))
	seen := make(map[string]bool, len(left)+len(right))
	for _, card := range append(left, right...) {
		id := strings.TrimSpace(card.SceneID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, card)
	}
	return out
}

func shouldUseCoverageSceneCards(intent queryIntent, summaries []SummaryContext, cards []store.SceneCardRow) bool {
	if !intentUsesBroadCoverage(intent) || hasVisibleSummaryContext(summaries) {
		return false
	}
	return len(cards) == 0 || intent == intentCharacterInventory || intent == intentStorySummary || intent == intentBroad
}

func summariesForIntent(summaries []SummaryContext, intent queryIntent, chapterID string) []SummaryContext {
	if len(summaries) == 0 {
		return nil
	}
	chapterID = strings.TrimSpace(chapterID)
	visible := make([]SummaryContext, 0, len(summaries))
	for _, summary := range summaries {
		if !isVisibleSummaryContext(summary) {
			continue
		}
		if chapterID != "" && summary.RecordType == "chapter_summary" && summary.ChapterID != chapterID {
			continue
		}
		visible = append(visible, summary)
	}
	if len(visible) == 0 {
		return nil
	}

	switch intent {
	case intentStorySummary:
		if book := firstSummaryOfType(visible, "book_summary"); book != nil {
			return []SummaryContext{*book}
		}
		return summariesOfType(visible, "chapter_summary")
	case intentChapterSummary:
		chapters := summariesOfType(visible, "chapter_summary")
		if len(chapters) > 0 {
			return chapters
		}
		if book := firstSummaryOfType(visible, "book_summary"); book != nil {
			return []SummaryContext{*book}
		}
	case intentCharacterArc, intentThemeStyle, intentBroad, intentEnding:
		if chapterID != "" {
			chapters := summariesOfType(visible, "chapter_summary")
			if len(chapters) > 0 {
				return chapters
			}
		}
		if book := firstSummaryOfType(visible, "book_summary"); book != nil {
			return []SummaryContext{*book}
		}
		return summariesOfType(visible, "chapter_summary")
	default:
		if chapterID != "" {
			chapters := summariesOfType(visible, "chapter_summary")
			if len(chapters) > 0 {
				return chapters
			}
		}
		if book := firstSummaryOfType(visible, "book_summary"); book != nil {
			return []SummaryContext{*book}
		}
	}
	return visible
}

func firstSummaryOfType(summaries []SummaryContext, recordType string) *SummaryContext {
	for _, summary := range summaries {
		if summary.RecordType == recordType {
			summaryCopy := summary
			return &summaryCopy
		}
	}
	return nil
}

func summariesOfType(summaries []SummaryContext, recordType string) []SummaryContext {
	out := make([]SummaryContext, 0, len(summaries))
	for _, summary := range summaries {
		if summary.RecordType == recordType {
			out = append(out, summary)
		}
	}
	return out
}

func characterRolesForIntent(roles []CharacterRoleContext, entities []EntityContext, question string, intent queryIntent, mode string) []CharacterRoleContext {
	if len(roles) == 0 || !wantsCharacterRoleContext(intent, mode) {
		return nil
	}
	normalized := normalizeQuestionText(question)
	entityIDs := make(map[string]bool, len(entities))
	for _, entity := range entities {
		if strings.TrimSpace(entity.Entity.ID) != "" {
			entityIDs[strings.TrimSpace(entity.Entity.ID)] = true
		}
	}

	type scoredRole struct {
		role  CharacterRoleContext
		score int
		index int
	}
	scored := make([]scoredRole, 0, len(roles))
	for i, role := range roles {
		score := characterRoleScore(normalized, role, entityIDs, intent)
		if score == 0 {
			continue
		}
		scored = append(scored, scoredRole{role: role, score: score, index: i})
	}
	if len(scored) == 0 {
		return nil
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		leftPriority := characterRolePriority(scored[i].role.Classification)
		rightPriority := characterRolePriority(scored[j].role.Classification)
		if leftPriority != rightPriority {
			return leftPriority < rightPriority
		}
		leftName := strings.ToLower(scored[i].role.CanonicalName)
		rightName := strings.ToLower(scored[j].role.CanonicalName)
		if leftName != rightName {
			return leftName < rightName
		}
		return scored[i].index < scored[j].index
	})
	limit := defaultCharacterRoleContextLimit
	if intent == intentCharacterInventory {
		limit = inventoryCharacterRoleContextLimit
	}
	if len(scored) > limit {
		scored = scored[:limit]
	}
	out := make([]CharacterRoleContext, len(scored))
	for i, item := range scored {
		out[i] = item.role
	}
	return out
}

func characterRoleScore(normalizedQuestion string, role CharacterRoleContext, entityIDs map[string]bool, intent queryIntent) int {
	score := 0
	if entityNameMentioned(normalizedQuestion, role.CanonicalName) {
		score += 100
	}
	for _, alias := range role.Aliases {
		if entityNameMentioned(normalizedQuestion, alias) {
			score += 80
			break
		}
	}
	for _, entityID := range role.SourceEntityIDs {
		if entityIDs[strings.TrimSpace(entityID)] {
			score += 70
			break
		}
	}
	if intent == intentCharacterInventory {
		score += 40 - characterRolePriority(role.Classification)
	}
	if intent == intentCharacterArc && isGenericCharacterArcQuestion(normalizedQuestion) && characterRolePriority(role.Classification) <= 1 {
		score += 25
	}
	return score
}

func characterRolePriority(classification string) int {
	switch strings.TrimSpace(strings.ToLower(classification)) {
	case "principal":
		return 0
	case "major_supporting":
		return 1
	case "supporting":
		return 2
	case "minor":
		return 3
	default:
		return 4
	}
}

func wantsCharacterRoleContext(intent queryIntent, mode string) bool {
	switch intent {
	case intentCharacterInventory, intentCharacterQuestion, intentCharacterArc:
		return true
	default:
		return characterMode(mode)
	}
}

func (p evidencePacket) validRecordIDs() map[string]bool {
	valid := make(map[string]bool)
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id != "" {
			valid[id] = true
		}
	}
	for _, id := range p.RecordsUsed {
		add(id)
	}
	for _, summary := range p.Summaries {
		add(summaryRecordID(summary))
		for _, source := range summary.SourceRecords {
			add(source)
		}
	}
	for _, role := range p.CharacterRoles {
		add(characterRoleRecordID(role))
		for _, entityID := range role.SourceEntityIDs {
			add(entityID)
		}
	}
	for _, entity := range p.EntityContext {
		add(entity.Entity.ID)
	}
	for _, digest := range p.Digests {
		add(digest.ID)
		for _, source := range digest.SourceRecords {
			add(source)
		}
	}
	for _, card := range p.SceneCards {
		add(card.SceneID)
	}
	return valid
}

func (p evidencePacket) defaultRecordsUsed() []string {
	var out []string
	seen := make(map[string]bool)
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, id)
	}
	for _, summary := range p.Summaries {
		add(summaryRecordID(summary))
	}
	for _, role := range p.CharacterRoles {
		add(characterRoleRecordID(role))
	}
	for _, entity := range p.EntityContext {
		add(entity.Entity.ID)
	}
	for _, digest := range p.Digests {
		add(digest.ID)
	}
	for _, card := range p.SceneCards {
		add(card.SceneID)
	}
	return out
}

func validateRecordIDs(ids []string, valid map[string]bool) []string {
	out := make([]string, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] || !valid[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func summaryRecordID(summary SummaryContext) string {
	switch strings.TrimSpace(summary.RecordType) {
	case "book_summary":
		return "book_summary"
	case "chapter_summary":
		if strings.TrimSpace(summary.ChapterID) != "" {
			return "chapter_summary:" + strings.TrimSpace(summary.ChapterID)
		}
		return "chapter_summary"
	default:
		if strings.TrimSpace(summary.RecordType) != "" {
			return strings.TrimSpace(summary.RecordType)
		}
	}
	return "summary"
}

func characterRoleRecordID(role CharacterRoleContext) string {
	if strings.TrimSpace(role.CharacterID) == "" {
		return ""
	}
	return "character_role:" + strings.TrimSpace(role.CharacterID)
}

func defaultRecordsUsedForIntent(intent queryIntent) bool {
	return intentUsesBroadCoverage(intent) || intent == intentCharacterInventory || intent == intentCharacterArc
}

func intentUsesBroadCoverage(intent queryIntent) bool {
	switch intent {
	case intentStorySummary, intentChapterSummary, intentCharacterInventory, intentCharacterArc, intentThemeStyle, intentEnding, intentBroad:
		return true
	default:
		return false
	}
}

func isSummaryQuestion(normalized string) bool {
	for _, phrase := range []string{"summary", "summarize", "synopsis", "recap", "overview", "what is the story about", "what is the book about", "what is the novel about"} {
		if containsQuestionPhrase(normalized, phrase) {
			return true
		}
	}
	return false
}

func isCharacterInventoryQuestion(normalized string) bool {
	for _, phrase := range []string{"main characters", "principal characters", "major characters", "all characters", "list characters", "list the characters", "who are the characters", "characters in the story", "cast of characters", "character list"} {
		if containsQuestionPhrase(normalized, phrase) {
			return true
		}
	}
	return false
}

func isCharacterArcQuestion(normalized string) bool {
	if isGenericCharacterArcQuestion(normalized) {
		return true
	}
	if !isChangeDevelopmentQuestion(normalized) {
		return false
	}
	for _, phrase := range []string{"how does", "how do", "does", "do"} {
		if containsQuestionPhrase(normalized, phrase) {
			return true
		}
	}
	return isCharacterQuestion(normalized)
}

func isGenericCharacterArcQuestion(normalized string) bool {
	for _, phrase := range []string{
		"character arc",
		"character arcs",
		"character development",
		"character develops",
		"characters develop",
		"characters change",
		"main characters change",
		"principal characters change",
		"protagonist arc",
		"protagonist change",
		"protagonist changes",
	} {
		if containsQuestionPhrase(normalized, phrase) {
			return true
		}
	}
	return false
}

func isChangeDevelopmentQuestion(normalized string) bool {
	for _, phrase := range []string{"change", "changes", "changed", "changing", "develop", "develops", "developed", "development", "evolve", "evolves", "evolved", "grow", "grows", "grew"} {
		if containsQuestionPhrase(normalized, phrase) {
			return true
		}
	}
	return false
}

func isThemeStyleQuestion(normalized, mode string) bool {
	mode = strings.TrimSpace(strings.ToLower(mode))
	if mode == "interpretation" || mode == "style" {
		return true
	}
	for _, phrase := range []string{"theme", "themes", "motif", "motifs", "symbol", "symbolism", "style", "voice", "tone", "structure"} {
		if containsQuestionPhrase(normalized, phrase) {
			return true
		}
	}
	return false
}

func isBroadStructuralQuestion(normalized string) bool {
	for _, phrase := range []string{"overall", "whole story", "story as a whole", "whole book", "whole novel", "broad", "across the story", "throughout the story", "entire story", "entire book", "entire novel"} {
		if containsQuestionPhrase(normalized, phrase) {
			return true
		}
	}
	return false
}
