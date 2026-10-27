package character

import "time"

// Tuning describes a persistent disposition, not the character's present mood.
// It changes the opportunity to act; each actual contact still needs a model
// decision and passes the contact policy at delivery time.
type Tuning struct {
	Initiative int `json:"initiative"`
}

type InitiativeProfile struct {
	Enabled            bool
	ConsiderationGap   time.Duration
	FirstContactGap    time.Duration
	FollowupGap        time.Duration
	MaxUnanswered      int
	RoutineDailyChecks int
	FreshDailyChecks   int
}

func ProfileForInitiative(level int) InitiativeProfile {
	if level <= 0 {
		return InitiativeProfile{}
	}
	if level > 100 {
		level = 100
	}
	profile := InitiativeProfile{Enabled: true, MaxUnanswered: 1}
	if level <= 25 {
		profile.ConsiderationGap = time.Duration(360-(level*240/25)) * time.Minute
		profile.FirstContactGap = time.Duration(120-(level*90/25)) * time.Minute
	} else if level < 75 {
		profile.ConsiderationGap = time.Duration(120-((level-25)*100/75)) * time.Minute
		profile.FirstContactGap = time.Duration(30-((level-25)*20/75)) * time.Minute
	} else {
		// The top end is an intentionally responsive disposition. Its upper
		// bound should be useful for observing natural follow-ups in one sitting.
		profile.ConsiderationGap = time.Duration(54-(level-75)*50/25) * time.Minute
		profile.FirstContactGap = time.Duration(17-(level-75)*15/25) * time.Minute
	}
	profile.FollowupGap = profile.ConsiderationGap
	// A highly outgoing character needs opportunities across a waking day.
	// Fewer checks than this exhaust in under an hour at the fastest cadence.
	profile.RoutineDailyChecks = 4 + (level-25)*44/75
	if profile.RoutineDailyChecks < 2 {
		profile.RoutineDailyChecks = 2
	}
	profile.FreshDailyChecks = profile.RoutineDailyChecks + 2
	switch {
	case level >= 90:
		profile.MaxUnanswered = 4
	case level >= 75:
		profile.MaxUnanswered = 3
	case level >= 50:
		profile.MaxUnanswered = 2
	}
	return profile
}
