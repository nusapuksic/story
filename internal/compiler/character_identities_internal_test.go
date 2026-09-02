package compiler

import (
	"strings"
	"testing"
)

func testCharacterIdentityInput() characterIdentityInput {
	return characterIdentityInput{
		SourceEntities: []characterIdentitySource{
			{EntityID: "entity-a", CanonicalName: "Mara", Aliases: []string{"Maraa"}, SurfaceTexts: []string{"Mara", "Maraa"}, EvidenceScenes: []string{"sc-a"}},
			{EntityID: "entity-b", CanonicalName: "Maraa", Aliases: []string{"Mara"}, SurfaceTexts: []string{"Maraa"}, EvidenceScenes: []string{"sc-b"}},
			{EntityID: "entity-c", CanonicalName: "Marin", EvidenceScenes: []string{"sc-c"}},
		},
		EntityByID: map[string]characterIdentitySource{
			"entity-a": {EntityID: "entity-a", CanonicalName: "Mara", Aliases: []string{"Maraa"}, SurfaceTexts: []string{"Mara", "Maraa"}, EvidenceScenes: []string{"sc-a"}},
			"entity-b": {EntityID: "entity-b", CanonicalName: "Maraa", Aliases: []string{"Mara"}, SurfaceTexts: []string{"Maraa"}, EvidenceScenes: []string{"sc-b"}},
			"entity-c": {EntityID: "entity-c", CanonicalName: "Marin", EvidenceScenes: []string{"sc-c"}},
		},
		SceneIDs: map[string]bool{"sc-a": true, "sc-b": true, "sc-c": true},
		ScenesByEntity: map[string]map[string]bool{
			"entity-a": {"sc-a": true}, "entity-b": {"sc-b": true}, "entity-c": {"sc-c": true},
		},
	}
}

func TestParseCharacterIdentityResponseVariantsAndDistinctGroups(t *testing.T) {
	input := testCharacterIdentityInput()
	response := `{"characters":[{"source_entity_ids":["entity-a","entity-b"],"canonical_name":"Mara","aliases":["Maraa"],"variants":[{"type":"possible_typo","value":"Maraa","source_entity_id":"entity-b","evidence":["sc-b"],"reason":"One spelling variant."}]},{"source_entity_ids":["entity-c"],"canonical_name":"Marin"}]}`
	records, err := parseCharacterIdentityResponse(response, input, nil, CharacterIdentityGeneration{})
	if err != nil {
		t.Fatalf("parse grouped identities: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("records = %#v, want merged pair plus distinct entity", records)
	}
	merged := records[0]
	if len(records[1].SourceEntityIDs) > len(merged.SourceEntityIDs) {
		merged = records[1]
	}
	if len(merged.SourceEntityIDs) != 2 {
		t.Fatalf("records = %#v, want one merged pair", records)
	}
	foundVariant := false
	for _, variant := range merged.Variants {
		if variant.Type == CharacterVariantPossibleTypo && variant.Value == "Maraa" {
			foundVariant = true
		}
	}
	if !foundVariant {
		t.Fatalf("possible typo evidence was not preserved: %#v", merged.Variants)
	}
	if records[0].CharacterID == records[1].CharacterID || !strings.HasPrefix(records[0].CharacterID, "char-") || !strings.HasPrefix(records[1].CharacterID, "char-") {
		t.Fatalf("IDs = %q, %q; want distinct char- IDs", records[0].CharacterID, records[1].CharacterID)
	}
}

func TestParseCharacterIdentityResponseRejectsInvalidCoverageAndNames(t *testing.T) {
	cases := []struct{ name, response, want string }{
		{"unknown source", `{"characters":[{"source_entity_ids":["entity-nope"],"canonical_name":"Mara"}]}`, "unknown source_entity_id"},
		{"duplicate source", `{"characters":[{"source_entity_ids":["entity-a","entity-a"],"canonical_name":"Mara"},{"source_entity_ids":["entity-b"],"canonical_name":"Maraa"},{"source_entity_ids":["entity-c"],"canonical_name":"Marin"}]}`, "appears more than once"},
		{"omitted source", `{"characters":[{"source_entity_ids":["entity-a"],"canonical_name":"Mara"},{"source_entity_ids":["entity-c"],"canonical_name":"Marin"}]}`, "omitted source_entity_id"},
		{"invalid canonical", `{"characters":[{"source_entity_ids":["entity-a"],"canonical_name":"Mary"},{"source_entity_ids":["entity-b"],"canonical_name":"Maraa"},{"source_entity_ids":["entity-c"],"canonical_name":"Marin"}]}`, "canonical_name"},
		{"invalid alias", `{"characters":[{"source_entity_ids":["entity-a"],"canonical_name":"Mara","aliases":["Mary"]},{"source_entity_ids":["entity-b"],"canonical_name":"Maraa"},{"source_entity_ids":["entity-c"],"canonical_name":"Marin"}]}`, "alias"},
		{"invalid evidence", `{"characters":[{"source_entity_ids":["entity-a"],"canonical_name":"Mara","variants":[{"type":"alias","value":"Maraa","source_entity_id":"entity-a","evidence":["sc-nope"]}]},{"source_entity_ids":["entity-b"],"canonical_name":"Maraa"},{"source_entity_ids":["entity-c"],"canonical_name":"Marin"}]}`, "unknown evidence_id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseCharacterIdentityResponse(tc.response, testCharacterIdentityInput(), nil, CharacterIdentityGeneration{})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestExistingCharacterIdentityIDReusesExactSourceSet(t *testing.T) {
	current := []CharacterIdentityRecord{{CharacterID: "char-existing", SourceEntityIDs: []string{"entity-b", "entity-a"}}}
	if got := existingCharacterIdentityID(current, []string{"entity-a", "entity-b"}); got != "char-existing" {
		t.Fatalf("reused ID = %q, want char-existing", got)
	}
	if got := existingCharacterIdentityID(current, []string{"entity-a", "entity-c"}); !strings.HasPrefix(got, "char-") || got == "char-existing" {
		t.Fatalf("new ID = %q, want fresh char- ID", got)
	}
}
