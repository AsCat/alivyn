package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/AsCat/alivyn/internal/character"
	"github.com/AsCat/alivyn/internal/life"
	"github.com/AsCat/alivyn/internal/store"
	"github.com/AsCat/alivyn/pkg/extension"
)

const actorSubject = "default"

type builtinFeedPerceptor struct{}

func (builtinFeedPerceptor) Observe(ctx context.Context, at time.Time) ([]extension.Observation, error) {
	candidates, err := life.Fetch(ctx, nil)
	if err != nil {
		return nil, err
	}
	observations := make([]extension.Observation, 0, len(candidates))
	for _, candidate := range candidates {
		payload, err := json.Marshal(candidate)
		if err != nil {
			return nil, err
		}
		observations = append(observations, extension.Observation{Kind: "public_feed_excerpt", At: at, Source: candidate.URL, MediaType: "text/plain", Payload: payload})
	}
	return observations, nil
}

func (s Session) readingCandidates(ctx context.Context, at time.Time) ([]life.Candidate, error) {
	var provider extension.Perceptor = builtinFeedPerceptor{}
	if s.Extensions != nil {
		if selected, ok := s.Extensions.Active("life.reading", extension.Perceive); ok {
			provider = selected.Perceptor
		}
	}
	observations, err := provider.Observe(ctx, at)
	if err != nil {
		return nil, err
	}
	if len(observations) > 40 {
		return nil, errors.New("too many reading observations")
	}
	candidates := make([]life.Candidate, 0, len(observations))
	for _, observed := range observations {
		if observed.Kind != "public_feed_excerpt" || observed.At.After(at) || observed.Source == "" {
			return nil, errors.New("invalid reading observation")
		}
		var candidate life.Candidate
		if err := json.Unmarshal(observed.Payload, &candidate); err != nil {
			return nil, err
		}
		link, err := url.Parse(candidate.URL)
		if err != nil || link.Scheme != "https" || link.Host == "" || candidate.URL != observed.Source || candidate.Title == "" || candidate.Excerpt == "" || len([]rune(candidate.Excerpt)) > 1200 {
			return nil, errors.New("invalid sourced reading candidate")
		}
		candidates = append(candidates, candidate)
	}
	return candidates, nil
}

type builtinMemory struct{ history *store.History }

func (m builtinMemory) Recall(ctx context.Context, q extension.RecallQuery) ([]extension.MemoryHit, error) {
	facts, err := m.history.CharacterSelfFactsBefore(ctx, q.BeforeMessageID)
	if err != nil {
		return nil, err
	}
	episodes, err := m.history.CharacterEpisodesBefore(ctx, q.BeforeMessageID)
	if err != nil {
		return nil, err
	}
	hits := make([]extension.MemoryHit, 0, len(facts)+len(episodes))
	for _, fact := range facts {
		fields, err := json.Marshal(fact)
		if err != nil {
			return nil, err
		}
		hits = append(hits, extension.MemoryHit{ID: "self:" + fact.Key, Kind: "self_fact", Content: fact.Value, Fields: fields})
	}
	for _, episode := range episodes {
		fields, err := json.Marshal(episode)
		if err != nil {
			return nil, err
		}
		hits = append(hits, extension.MemoryHit{ID: "episode:" + episode.SourceIDs[0], Kind: "episode", Content: episode.Event, SourceIDs: episode.SourceIDs, Fields: fields})
	}
	return hits, nil
}

type builtinAffect struct{ history *store.History }

type continuityProjection struct {
	Moment      character.Moment       `json:"moment"`
	OpenThreads []character.OpenThread `json:"open_threads"`
}

func (m builtinAffect) Project(ctx context.Context, q extension.ProjectionRequest) (extension.ProjectionResult, error) {
	state, at, err := m.history.CharacterStateBefore(ctx, q.BeforeMessageID, q.AsOf)
	if err != nil {
		return extension.ProjectionResult{}, err
	}
	value, err := json.Marshal(continuityProjection{Moment: state.Moment, OpenThreads: state.OpenThreads})
	if err != nil {
		return extension.ProjectionResult{}, err
	}
	return extension.ProjectionResult{Namespace: "character.continuity", Version: "1", Value: value, At: at}, nil
}

// characterMemory is the first live module seam: an optional recall provider
// may replace the built-in lookup without changing the character generator.
// The core still owns transcript commits, forgetting, and source boundaries.
func (s Session) characterMemory(ctx context.Context, sourceID int64, now time.Time) ([]character.SelfFact, []character.Episode, error) {
	var provider extension.Retriever = builtinMemory{s.History}
	if s.Extensions != nil {
		if selected, ok := s.Extensions.Active("character.memory", extension.Recall); ok {
			provider = selected.Retriever
		}
	}
	hits, err := provider.Recall(ctx, extension.RecallQuery{SubjectID: actorSubject, BeforeMessageID: sourceID, AsOf: now, Limit: 8})
	if err != nil {
		return nil, nil, err
	}
	if len(hits) > 32 {
		return nil, nil, errors.New("character memory result exceeds the context budget")
	}
	var facts []character.SelfFact
	var episodes []character.Episode
	for _, hit := range hits {
		if hit.ID == "" || len(hit.Fields) == 0 || len(hit.Fields) > 4096 || len([]rune(hit.Content)) > 500 {
			return nil, nil, fmt.Errorf("memory %q has invalid or oversized fields", hit.ID)
		}
		switch hit.Kind {
		case "self_fact":
			var fact character.SelfFact
			if err := json.Unmarshal(hit.Fields, &fact); err != nil || fact.Key == "" || fact.Value == "" {
				return nil, nil, errors.New("invalid self-fact memory")
			}
			facts = append(facts, fact)
		case "episode":
			var episode character.Episode
			if err := json.Unmarshal(hit.Fields, &episode); err != nil || episode.Event == "" || len(episode.SourceIDs) == 0 {
				return nil, nil, errors.New("invalid episode memory")
			}
			episodes = append(episodes, episode)
		default:
			return nil, nil, fmt.Errorf("unsupported character memory kind %q", hit.Kind)
		}
	}
	return facts, episodes, nil
}

// characterAffect is a replaceable state projection. Selection is explicit;
// the existing SQLite projection remains the default for every installation.
func (s Session) characterAffect(ctx context.Context, sourceID int64, now time.Time) (character.Moment, []character.OpenThread, time.Time, error) {
	var provider extension.Projector = builtinAffect{s.History}
	if s.Extensions != nil {
		if selected, ok := s.Extensions.Active("character.affect", extension.Project); ok {
			provider = selected.Projector
		}
	}
	result, err := provider.Project(ctx, extension.ProjectionRequest{SubjectID: actorSubject, BeforeMessageID: sourceID, AsOf: now})
	if err != nil {
		return character.Moment{}, nil, time.Time{}, err
	}
	if result.Namespace != "character.continuity" || result.Version != "1" || (!result.At.IsZero() && result.At.After(now)) {
		return character.Moment{}, nil, time.Time{}, errors.New("invalid character continuity projection")
	}
	if len(result.Value) == 0 || len(result.Value) > 16384 {
		return character.Moment{}, nil, time.Time{}, errors.New("character continuity projection exceeds the context budget")
	}
	var state continuityProjection
	if err := json.Unmarshal(result.Value, &state); err != nil {
		return character.Moment{}, nil, time.Time{}, err
	}
	return state.Moment, state.OpenThreads, result.At, nil
}
