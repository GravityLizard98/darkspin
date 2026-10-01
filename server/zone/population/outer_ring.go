package population

import "strings"

// Area themes select lieutenants, but Outer Rings' ordinary population must
// include all three small enemy species in equal shares (within one spawn).
func balanceOuterRingMinions(plans []candidate) {
	nouns := [...]string{
		"ZelemBasicChargeup.Noun",
		"ZelemBasicFlyingMelee.Noun",
		"CitadelSpecificThree.Noun",
	}
	nextMinionIndex := 0
	for planIndex := range plans {
		for nounIndex, noun := range plans[planIndex].provisionalNounNames {
			switch strings.ToLower(noun) {
			case "zelembasicchargeup.noun", "zelembasicflyingmelee.noun",
				"citadelspecificthree.noun":
				plans[planIndex].provisionalNounNames[nounIndex] =
					nouns[nextMinionIndex%len(nouns)]
				nextMinionIndex++
			}
		}
	}
}
