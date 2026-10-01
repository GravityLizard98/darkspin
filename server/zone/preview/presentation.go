package preview

import (
	"strings"

	"github.com/darkspinnet/darkspin/server/util"
)

type Presentation struct {
	CurrentMovie uint32
	CurrentVoice uint32
	NextMovie    uint32
}

type campaignScene struct {
	chainLevelIndex uint32
	movieName       string
	voiceName       string
}

var campaignScenes = []campaignScene{
	{chainLevelIndex: 1, movieName: "cam_fmv_02_zelems"},
	{chainLevelIndex: 3, movieName: "cam_fmv_03_nocturna"},
	{chainLevelIndex: 5, movieName: "cam_fmv_04_verdanth"},
	{chainLevelIndex: 7, voiceName: "vo_ship_flow_reinfect_zelems"},
	{chainLevelIndex: 9, movieName: "cam_fmv_05_cryos"},
	{chainLevelIndex: 12, voiceName: "vo_ship_flow_reinfect_verdanth"},
	{chainLevelIndex: 13, movieName: "cam_fmv_06_infinity"},
	{chainLevelIndex: 16, voiceName: "vo_ship_flow_reinfect_cryos"},
	{chainLevelIndex: 18, voiceName: "vo_ship_flow_reinfect_nocturna"},
	{chainLevelIndex: 20, voiceName: "vo_ship_flow_reinfect_infinity"},
	{chainLevelIndex: 21, movieName: "cam_fmv_07_scaldron"},
}

var missionVoiceNamesByLevel = map[string]string{
	"zelems_1":   "vo_ship_pip_zelem_1b",
	"zelems_2":   "vo_ship_pip_zelem_2b",
	"zelems_3":   "vo_ship_pip_zelem_3a",
	"zelems_4":   "vo_ship_pip_zelem_4b",
	"nocturna_1": "vo_ship_pip_nocturna_1b",
	"nocturna_2": "vo_ship_pip_nocturna_2b",
	"nocturna_3": "vo_ship_pip_nocturna_3b",
	"nocturna_4": "vo_ship_pip_nocturna_4a",
	"verdanth_1": "vo_ship_pip_verdanth_1b",
	"verdanth_2": "vo_ship_pip_verdanth_2b",
	"verdanth_3": "vo_ship_pip_verdanth_3b",
	"verdanth_4": "vo_ship_pip_verdanth_4c",
	"cryos_1":    "vo_ship_pip_cryos_1b",
	"cryos_2":    "vo_ship_pip_cryos_2a",
	"cryos_3":    "vo_ship_pip_cryos_3a",
	"cryos_4":    "vo_ship_pip_cryos_4a",
	"infinity_1": "vo_ship_pip_infinity_1b",
	"infinity_2": "vo_ship_pip_infinity_2b",
	"infinity_3": "vo_ship_pip_infinity_3a",
	"infinity_4": "vo_ship_pip_infinity_4b",
	"scaldron_1": "vo_ship_pip_scaldron_1b",
	"scaldron_2": "vo_ship_pip_scaldron_2a",
	"scaldron_3": "vo_ship_pip_scaldron_3b",
	"scaldron_4": "vo_ship_pip_scaldron_4a",
}

const campaignEpilogueMovieName = "cam_fmv_08_epilogue"

// CampaignPresentation selects only story scenes authored for the current or
// immediately following first-pass chain slot. Later difficulty passes reuse
// maps without replaying the first-pass planet introductions. The epilogue is
// offered only after the first-pass finale has completed.
func CampaignPresentation(
	chainLevelIndex uint32, isCompleted bool,
) Presentation {
	presentation := Presentation{}
	if chainLevelIndex == 0 || chainLevelIndex > 24 {
		return presentation
	}
	for _, scene := range campaignScenes {
		if scene.chainLevelIndex == chainLevelIndex {
			if scene.movieName != "" {
				presentation.CurrentMovie = util.HashID(scene.movieName)
			}
			if scene.voiceName != "" {
				presentation.CurrentVoice = util.HashID(scene.voiceName)
			}
		}
		if scene.chainLevelIndex == chainLevelIndex+1 && scene.movieName != "" {
			presentation.NextMovie = util.HashID(scene.movieName)
		}
	}
	if isCompleted && chainLevelIndex == 24 {
		presentation.NextMovie = util.HashID(campaignEpilogueMovieName)
	}
	return presentation
}

// CampaignEntryPresentation selects only a movie authored on the level being
// entered for the first time. The result transition owns any following scene;
// entry must not pull that scene backward or invent a cue for a silent level.
func CampaignEntryPresentation(
	chainLevelIndex uint32, chainProgression uint32,
) Presentation {
	if chainLevelIndex == 0 || chainProgression >= chainLevelIndex {
		return Presentation{}
	}
	presentation := CampaignPresentation(chainLevelIndex, false)
	presentation.NextMovie = 0
	return presentation
}

func CampaignMissionVoice(level string) uint32 {
	level = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(level)), ".level")
	voiceName := missionVoiceNamesByLevel[level]
	if voiceName == "" {
		return 0
	}
	return util.HashID(voiceName)
}
