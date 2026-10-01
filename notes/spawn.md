Finish: 



Yes—there are explicit first-time encounter hints in 1-1’s data, and our current implementation loses several of them. I saved the [full analysis and evidence](D:/src/darkspin/bin/game/logs/zelems1-population/review.txt).
What the map actually tells us:
Authored data	What it establishes
Separate firstTimeConfig	A distinct enemy roster for first-time progression
Per-marker sectionType	Whether a point belongs to pacing section A, B, C, or “Any”
Marker-set groups and weights	Alternative encounter layouts
HasBeatenThisLevel checks in Lua	Progress-dependent ability unlocks and encounter timing


At difficulty 1–24, the first-time roster explicitly contains three minion types—ZelemBasicRanged, ZelemBasicHybrid, ZelemBasicRepair—and three special types: ZelemSpecialHaster, NomadSnipe, NomadWithDrone.
There are 341 wanderer candidates and 54 spike candidates. These are possible spawn locations; their presence does not guarantee an enemy at every point.
I found four significant problems in our handling:
1. The importer loses the first-time roster boundary. It flattens both configurations together and mislabels three populated pools. We therefore aren’t faithfully preserving that authored distinction.
2. We ignore the markers’ actual sections. The server guesses from the filename’s final character, making SpikeC2 section A and SpikeB2c section C. Against the explicit fields, 25 points receive the wrong concrete section; another three lose their “Any” designation. Even correcting filename parsing would miss authored exceptions.
3. We lose encounter alternatives. SpikeB2, SpikeB2b, and SpikeB2c share one group with equal weights. The horde group also contains an empty alternative, weighted 5 against the two populated alternatives’ weights of 2 each. Our projection drops that empty choice.
4. Our first-clear placement is currently an approximation. It overlays eight hard-coded formations onto another population plan. It isn’t a recovered, authored first-run layout.
So we can recover which enemy types are eligible, which route sections their points belong to, and which layouts are alternatives. I found no per-point “spawn on first clear” flag that establishes every occupied location. The exact selection still depends on director rules that haven’t been fully recovered.
The strongest next step is to preserve those authored fields before further tuning placement. Otherwise we’re adjusting encounters using incorrectly grouped locations.

## Implementation update (2026-10-01)

- The 1-1 importer now labels populated entries by both configuration (`levelConfig` or `firstTimeConfig`) and role. Campaign setup filters them by difficulty; first-clear population uses the first-time minion and special pools. The absent agent pool is no longer inferred from mislabeled minions: 1-1 horde and boss adds explicitly draw from eligible minions.
- SpawnPointDef section and activatesSpike are imported per wanderer or spike marker. Population uses the explicit A/B/C/Any section; filename parsing is only a fallback for directors without that field.
- Marker-set groups and weights reach the campaign projection, including the empty Horde_4 set. The 1-1 spike group selects one weighted alternative per population session. The horde group remains exposed as authored alternatives; encounter activation still follows its existing scripted route because the native condition/selection policy has not been recovered.
- The eight synthetic first-clear formations have been removed. First-clear population selects positions from authored candidates and nouns from the difficulty-filtered first-time roster. Its budget and point occupancy remain local approximations, not a recovered first-clear layout.
