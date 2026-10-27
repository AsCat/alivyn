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
	} else {
		profile.ConsiderationGap = time.Duration(120-((level-25)*100/75)) * time.Minute
		profile.FirstContactGap = time.Duration(30-((level-25)*20/75)) * time.Minute
	}
	profile.FollowupGap = profile.ConsiderationGap
	profile.RoutineDailyChecks = 4 + (level-25)*8/75
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
