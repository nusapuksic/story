package compiler_test

import (
	"context"
	"github.com/nusapuksic/story/internal/compiler"
	"github.com/nusapuksic/story/internal/project"
	"github.com/nusapuksic/story/internal/provider"
	"strings"
	"testing"
)

func TestIdentitySmoke(t *testing.T) {
	p, st := buildTestProject(t)
	scenes := seedEntityReverseIndex(t, p, st)
	ep := &fakeProvider{response: `{"entities":[{"canonical_name":"Mara","type":"character","occurrences":[{"scene_id":"` + scenes[0].ID + `","surface_texts":["Mara"],"confidence":0.9}]}]}`}
	if _, err := compiler.Compile(context.Background(), p, st, compiler.Options{Layer: compiler.LayerEntities, ExtractionProvider: ep, ExtractionModel: "fake"}); err != nil {
		t.Fatal(err)
	}
	ip := &fakeProvider{responseFunc: func(req provider.GenerationRequest, idx int) string {
		for _, line := range strings.Split(req.Messages[1].Content, "\n") {
			if strings.Contains(line, "entity_id:") {
				id := strings.TrimSpace(strings.Split(strings.TrimPrefix(line, "- entity_id: "), ";")[0])
				return "{\"characters\":[{\"source_entity_ids\":[\"" + id + "\"],\"canonical_name\":\"Mara\",\"variants\":[{\"type\":\"alias\",\"value\":\"Mara\",\"source_entity_id\":\"" + id + "\",\"evidence\":[\"" + scenes[0].ID + "\"]}]}]}"
			}
		}
		return "{}"
	}}
	if _, err := compiler.Compile(context.Background(), p, st, compiler.Options{Layer: compiler.LayerCharacterIdentities, ExtractionProvider: ip, ExtractionModel: "fake"}); err != nil {
		t.Fatal(err)
	}
	roleProvider := &fakeProvider{responseFunc: func(req provider.GenerationRequest, idx int) string {
		for _, line := range strings.Split(req.Messages[1].Content, "\n") {
			if strings.Contains(line, "character_id:") {
				id := strings.TrimSpace(strings.Split(strings.TrimPrefix(line, "- character_id: "), ";")[0])
				return "{\"characters\":[{\"character_id\":\"" + id + "\",\"classification\":\"principal\",\"rationale\":\"drives the story\",\"evidence\":[{\"scene_id\":\"" + scenes[0].ID + "\",\"reason\":\"scene action\"}]}]}"
			}
		}
		return "{}"
	}}
	if _, err := compiler.Compile(context.Background(), p, st, compiler.Options{Layer: compiler.LayerPrincipals, ExtractionProvider: roleProvider, ExtractionModel: "fake"}); err != nil {
		t.Fatal(err)
	}
	rs, snap, err := compiler.ReadLatestCharacterIdentities(p.Path(project.ModelDir + "/character_identities.jsonl"))
	if err != nil || snap == nil || len(rs) != 1 {
		t.Fatalf("%#v %#v %v", rs, snap, err)
	}
	firstID := rs[0].CharacterID
	if _, err := compiler.Compile(context.Background(), p, st, compiler.Options{Layer: compiler.LayerEntities, ExtractionProvider: ep, ExtractionModel: "fake"}); err != nil {
		t.Fatal(err)
	}
	if _, err := compiler.Compile(context.Background(), p, st, compiler.Options{Layer: compiler.LayerCharacterIdentities, Force: true, ExtractionProvider: ip, ExtractionModel: "fake"}); err != nil {
		t.Fatal(err)
	}
	rs, _, err = compiler.ReadLatestCharacterIdentities(p.Path(project.ModelDir + "/character_identities.jsonl"))
	if err != nil || len(rs) != 1 || rs[0].CharacterID != firstID {
		t.Fatalf("identity ID not reused: first=%s records=%#v err=%v", firstID, rs, err)
	}

}
