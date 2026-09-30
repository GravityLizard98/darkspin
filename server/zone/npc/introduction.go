package npc

import "strings"

// Keep a boss's entrance independent of the distance-selected opening attack.
func WithBossIntroduction(plan SpawnPlan, profile ActionProfile) ActionProfile {
	if plan.OwnerObjectID != 0 {
		return profile
	}
	name := strings.TrimSuffix(strings.ToLower(plan.NounName), ".noun")
	name = strings.TrimSuffix(strings.TrimSuffix(name, "_2"), "_3")
	switch name {
	case "shadowboss", "zelemboss", "verdanthboss", "cryosboss", "citadelboss", "scaldronboss":
	default:
		return profile
	}
	intro, isFound := ActionProfileForNoun(plan.NounName)
	if !isFound {
		intro, isFound = OrcusFallbackProfile(plan.NounName)
	}
	if !isFound {
		return profile
	}
	profile.FirstAggroAnimationName = intro.FirstAggroAnimationName
	profile.FirstAggroAbilityName = intro.FirstAggroAbilityName
	profile.FirstAggroEffectName = intro.FirstAggroEffectName
	profile.FirstAggroEffectDelay = intro.FirstAggroEffectDelay
	profile.FirstAggroDelay = intro.FirstAggroDelay
	profile.FirstAggroRevealDelay = intro.FirstAggroRevealDelay
	profile.FirstAggroCinematicDuration = intro.FirstAggroCinematicDuration
	profile.FirstAggroCinematicRadius = intro.FirstAggroCinematicRadius
	profile.IsFirstAggroDurationKnown = intro.IsFirstAggroDurationKnown
	return profile
}

// mapIntroductionProfile retains ordinary aggro reactions but omits arrival
// animations for actors that are already standing in the map.
func mapIntroductionProfile(plan SpawnPlan, profile ActionProfile) ActionProfile {
	profile = WithBossIntroduction(plan, profile)
	if plan.Introduction != SpawnIntroductionDormant || plan.OwnerObjectID != 0 || plan.IsBoss {
		return profile
	}
	switch profile.FirstAggroAnimationName {
	case "gen_aggro_sp_beam_in", "zelem_flying_melee_beamin":
		profile.FirstAggroAnimationName = ""
		profile.FirstAggroDelay = 0
	}
	return profile
}
