package services

import (
	"context"
	"encoding/json"
	"message-consolidator/store"
	"strings"
)

type GraphData struct {
	Nodes []Node `json:"nodes"`
	Links []Edge `json:"links"`
}

type Node struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Value    float64 `json:"value"`
	IsMe     bool    `json:"is_me"`
	Category string  `json:"category"`
}

type Edge struct {
	Source string  `json:"source"`
	Target string  `json:"target"`
	Weight float64 `json:"weight"`
}

type nodeMeta struct {
	Name string
	Cat  string
}

func (s *ReportsService) getVisualizationJSON(ctx context.Context, email string, logs []Log) string {
	// Why: Manual aggregation uses RequesterCanonical as node ID, correctly unifying all aliases resolved by sanitizeMessages.
	// In-process aggregation only — no AI call here. The Gemini-backed GenerateVisualizationData
	// has its own reportID parameter for the day it gets wired into this path.
	vizData := s.generateVisualizationData(ctx, email, logs)
	b, _ := json.Marshal(vizData)
	return string(b)
}

// generateVisualizationData constructs a weighted network graph from logs.
func (s *ReportsService) generateVisualizationData(ctx context.Context, email string, messages []Log) GraphData {
	counts, pairWeights, meta := s.aggregateRelationsAlt(ctx, email, messages)
	nodes := make([]Node, 0)
	for id, val := range counts {
		nodes = append(nodes, Node{
			ID: id, Name: meta[id].Name,
			Value: val, IsMe: strings.EqualFold(id, email), Category: meta[id].Cat,
		})
	}
	links := make([]Edge, 0)
	for pair, weight := range pairWeights {
		parts := strings.Split(pair, "|")
		links = append(links, Edge{Source: parts[0], Target: parts[1], Weight: weight})
	}
	return GraphData{Nodes: nodes, Links: links}
}

// stripParenSuffix removes parenthetical content (e.g. "(JJ)", "(Ambiguous)") while preserving original case.
func stripParenSuffix(s string) string {
	if i := strings.Index(s, "("); i > 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

func (s *ReportsService) aggregateRelationsAlt(ctx context.Context, email string, messages []Log) (map[string]float64, map[string]float64, map[string]nodeMeta) {
	counts := make(map[string]float64)
	pairWeights := make(map[string]float64)
	meta := make(map[string]nodeMeta)
	for _, m := range messages {
		rID, rName, rCat := s.resolveRelationActor(ctx, email, m.RequesterCanonical, m.RequesterDisplayName, m.RequesterType, m.Requester)
		aID, aName, aCat := s.resolveRelationActor(ctx, email, m.AssigneeCanonical, m.AssigneeDisplayName, m.AssigneeType, m.Assignee)
		if rID == "" || aID == "" || rID == aID {
			continue
		}
		counts[rID]++
		counts[aID]++
		pairWeights[rID+"|"+aID]++
		meta[rID] = nodeMeta{rName, rCat}
		meta[aID] = nodeMeta{aName, aCat}
	}
	return counts, pairWeights, meta
}

// Why: Prefer the persisted canonical/display/category triple, but fall back to NormalizeWithCategory when the canonical ID is missing
// or when the persisted category is "External" with no explicit type — that fallback is the only path that re-classifies a contact.
func (s *ReportsService) resolveRelationActor(ctx context.Context, email, canonicalID, displayName, contactType, raw string) (string, string, string) {
	id := canonicalID
	name := displayName
	cat := s.resolveCategory(email, id, contactType)
	switch {
	case id == "":
		id, name, cat = store.NormalizeWithCategory(ctx, email, raw)
	case cat == "External" && contactType == "":
		fallback := displayName
		if fallback == "" {
			fallback = raw
		}
		if _, _, c := store.NormalizeWithCategory(ctx, email, fallback); c != "External" {
			cat = c
		}
	}
	if name == "" {
		name = raw
	}
	return id, stripParenSuffix(name), cat
}
