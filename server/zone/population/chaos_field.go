package population

import "strings"

// Keep Chaos Fields' three ordinary species equally represented (within one
// spawn), independently of each area's lieutenant theme.
func balanceChaosFieldMinions(plans []candidate) {
	nouns := [...]string{
		"ZelemBasicPackfly.Noun",
		"VerdanthBasicMelee.Noun",
		"Shooter.Noun",
	}
	nextMinionIndex := 0
	for planIndex := range plans {
		for nounIndex, noun := range plans[planIndex].provisionalNounNames {
			switch strings.ToLower(noun) {
			case "zelembasicpackfly.noun", "verdanthbasicmelee.noun", "shooter.noun":
				plans[planIndex].provisionalNounNames[nounIndex] =
					nouns[nextMinionIndex%len(nouns)]
				nextMinionIndex++
			}
		}
	}
}
