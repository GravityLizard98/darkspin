#include "command_catalog.h"

#include <string.h>

/* Keep the accepted destination names at the client compatibility boundary.
   These are the complete build-103 Level resources indexed from Levels.package;
   the server receives only an exact canonical name selected here. */
const char* const fang_warp_locations[] = {
    "Creature_Vid_Capture",
    "CreatureEditor_EL",
    "cryos_1",
    "cryos_1_SM",
    "cryos_2",
    "cryos_2_PVP",
    "cryos_3",
    "cryos_4",
    "Game_Tutorial_cryos_1",
    "front_end_ship",
    "infinity_1",
    "infinity_1_PVP",
    "infinity_2",
    "infinity_3",
    "infinity_4",
    "infinity_4_SM",
    "Juggernaut_Mode_Testing",
    "nocturna_1",
    "nocturna_2",
    "nocturna_2_PVP",
    "nocturna_3",
    "nocturna_3_SM",
    "nocturna_4",
    "scaldron_1",
    "scaldron_1_PVP",
    "scaldron_2",
    "scaldron_3",
    "scaldron_4",
    "scaldron_4_SM",
    "Spectra_1",
    "Spectra_2",
    "Spectra_3",
    "Spectra_4",
    "test_AI_arena",
    "test_AI_zoo",
    "test_AI_zoo_bio",
    "test_AI_zoo_chrono",
    "test_AI_zoo_cyber",
    "test_AI_zoo_elites",
    "test_AI_zoo_plasma",
    "test_AI_zoo_supernatural",
    "test_AI_zoo_ugc",
    "test_Creature_Vid_Capture",
    "test_holodeck",
    "test_survivor_arena",
    "test_VFX_arena",
    "tnx173_2",
    "tnx173_3",
    "TNX_173",
    "verdanth_1",
    "verdanth_2",
    "verdanth_3",
    "verdanth_3_PVP",
    "verdanth_4",
    "verdanth_4_SM",
    "zelems_1",
    "zelems_1_SM",
    "zelems_2",
    "zelems_2_PVP",
    "zelems_3",
    "zelems_4"
};
const size_t fang_warp_location_count =
    sizeof(fang_warp_locations) / sizeof(fang_warp_locations[0]);

/* Number only the disconnected and special-purpose areas surfaced by the
   level catalog. Keep this order aligned with the compact server chat listing;
   named matching still uses the complete location list above. */
const char* const fang_warp_aliases[] = {
    "Game_Tutorial_cryos_1",
    "CreatureEditor_EL",
    "Creature_Vid_Capture",
    "cryos_1_SM",
    "cryos_2_PVP",
    "front_end_ship",
    "infinity_1_PVP",
    "infinity_4_SM",
    "Juggernaut_Mode_Testing",
    "nocturna_2_PVP",
    "nocturna_3_SM",
    "scaldron_1_PVP",
    "scaldron_4_SM",
    "Spectra_1",
    "Spectra_2",
    "Spectra_3",
    "Spectra_4",
    "test_AI_arena",
    "test_AI_zoo",
    "test_AI_zoo_bio",
    "test_AI_zoo_chrono",
    "test_AI_zoo_cyber",
    "test_AI_zoo_elites",
    "test_AI_zoo_plasma",
    "test_AI_zoo_supernatural",
    "test_AI_zoo_ugc",
    "test_Creature_Vid_Capture",
    "test_holodeck",
    "test_survivor_arena",
    "test_VFX_arena",
    "TNX_173",
    "tnx173_2",
    "tnx173_3",
    "verdanth_3_PVP",
    "verdanth_4_SM",
    "zelems_1_SM",
    "zelems_2_PVP"
};
const size_t fang_warp_alias_count =
    sizeof(fang_warp_aliases) / sizeof(fang_warp_aliases[0]);

/* Keep developer NPC admission at the client compatibility boundary too.
   These are packaged, targetable combat families used by campaign, tutorial,
   captain, and Destructor content. The server still validates the selected
   canonical noun against the active warped zone's imported class catalog. */
const char* const fang_spawn_nouns[] = {
    "BabyMortar.Noun",
    "Boomer.Noun",
    "CitadelBasicGunner.Noun",
    "CitadelBasicMelee.Noun",
    "CitadelBasicRanged.Noun",
    "CitadelBasicShield.Noun",
    "CitadelBasicSuicide.Noun",
    "CitadelBoss.Noun",
    "CitadelBoss_2.Noun",
    "CitadelBoss_3.Noun",
    "CitadelSpecificFour.Noun",
    "CitadelSpecificOne.Noun",
    "CitadelSpecificThree.Noun",
    "CitadelSpecificTwo.Noun",
    "CitadelSpecialFour.Noun",
    "CitadelSpecialFour_Captain.Noun",
    "CitadelSpecialThree.Noun",
    "CitadelSpecialThree_Captain.Noun",
    "CitadelSpecialTwo.Noun",
    "CitadelSpecialTwo_Captain.Noun",
    "CryosBasicCharge.Noun",
    "CryosBasicFiery.Noun",
    "CryosBasicFireWave.Noun",
    "CryosBasicLightningMelee.Noun",
    "CryosBasicLightningRanged.Noun",
    "CryosBasicMelee.Noun",
    "CryosBasicPoison.Noun",
    "CryosBasicRanged.Noun",
    "CryosBoss.Noun",
    "CryosBoss_2.Noun",
    "CryosBoss_3.Noun",
    "CryosElementalSpecialThree.Noun",
    "CryosElementalSpecialThree_Captain.Noun",
    "CryosSpecialOne.Noun",
    "CryosSpecialOne_Captain.Noun",
    "CryosSpecialThree.Noun",
    "CryosSpecialThree_Captain.Noun",
    "CryosSpecialTwo.Noun",
    "CryosSpecialTwo_Captain.Noun",
    "MutationAgent.Noun",
    "nct_lieu_su_stealther.Noun",
    "nct_lieu_su_stealther_Captain.Noun",
    "nct_minn_su_drainer.Noun",
    "NoctBasicFlyer.Noun",
    "NoctBasicGhostCharger.Noun",
    "NoctBasicHopper.Noun",
    "NoctBasicMeleeDog.Noun",
    "NocturnaBasicHealthDrain.Noun",
    "NocturnaBasicRangedSilence.Noun",
    "NocturnaBasicStealth.Noun",
    "NocturnaSpecialDrift.Noun",
    "NocturnaSpecialHomer.Noun",
    "NocturnaSpecialLeech.Noun",
    "NocturnaSpecialLeech_Captain.Noun",
    "NocturnaSpecialMunch.Noun",
    "NocturnaSpecialMunch_Captain.Noun",
    "NomadBioSpecialTwo.Noun",
    "NomadBioSpecialTwo_Captain.Noun",
    "NomadCyberOne.Noun",
    "NomadDrag.Noun",
    "NomadRuption.Noun",
    "NomadRuption_Captain.Noun",
    "NomadScope.Noun",
    "NomadScope_Captain.Noun",
    "NomadShielder.Noun",
    "NomadShielder_Captain.Noun",
    "NomadSnipe.Noun",
    "NomadSnipe_Captain.Noun",
    "NomadSpacetimeAgent.Noun",
    "NomadSpecialOne.Noun",
    "NomadSpecialOne_Captain.Noun",
    "NomadSpecialThree.Noun",
    "NomadWithDrone.Noun",
    "NomadWithDrone_Captain.Noun",
    "Rezzer.Noun",
    "Rezzer_Captain.Noun",
    "ScaldronBasicBlink.Noun",
    "ScaldronBasicCopter.Noun",
    "ScaldronBasicDog.Noun",
    "ScaldronBasicDoppler.Noun",
    "ScaldronBasicMaser.Noun",
    "ScaldronBasicMines.Noun",
    "ScaldronBasicMonk.Noun",
    "ScaldronBasicNestle.Noun",
    "ScaldronBasicSinkhole.Noun",
    "ScaldronBasicThorno.Noun",
    "ScaldronBoss.Noun",
    "ScaldronBoss_2.Noun",
    "ScaldronBoss_3.Noun",
    "ShadowBoss.Noun",
    "ShadowBoss_2.Noun",
    "ShadowBoss_3.Noun",
    "ShadowBossMinion.Noun",
    "Shooter.Noun",
    "Sloth.Noun",
    "TutorialBasicDiseased.Noun",
    "TutorialBasicPoison.Noun",
    "TutorialBasicRanged.Noun",
    "TutorialSloth.Noun",
    "TutorialSpecialOne.Noun",
    "VerdanthBasicDiseased.Noun",
    "VerdanthBasicHealer.Noun",
    "VerdanthBasicMelee.Noun",
    "VerdanthBasicOoze.Noun",
    "VerdanthBasicPicky.Noun",
    "VerdanthBasicPlunge.Noun",
    "VerdanthBasicRanged.Noun",
    "VerdanthBasicRootmob.Noun",
    "VerdanthBasicSkeet.Noun",
    "VerdanthBoss.Noun",
    "VerdanthBoss_2.Noun",
    "VerdanthBoss_3.Noun",
    "VerdanthSpecialOne.Noun",
    "VerdanthSpecialOne_Captain.Noun",
    "VerdanthSpecialThree.Noun",
    "VerdanthSpecialThree_Captain.Noun",
    "VerdanthSpecialTwo.Noun",
    "VerdanthSpecialTwo_Captain.Noun",
    "ZelemBasicChargeup.Noun",
    "ZelemBasicFlyingMelee.Noun",
    "ZelemBasicHybrid.Noun",
    "ZelemBasicMelee.Noun",
    "ZelemBasicPackfly.Noun",
    "ZelemBasicPackMelee.Noun",
    "ZelemBasicRanged.Noun",
    "ZelemBasicRangedHoming.Noun",
    "ZelemBasicRepair.Noun",
    "ZelemBoss.Noun",
    "ZelemBoss_2.Noun",
    "ZelemBoss_3.Noun",
    "ZelemSpecialHaster.Noun",
    "ZelemSpecialHaster_Captain.Noun",
    "ZelemSpecialOne.Noun",
    "ZelemSpecialOne_Captain.Noun",
    "ZelemSpecialThree.Noun",
    "ZelemSpecialTwo.Noun",
    "ZelemSpecialTwo_Captain.Noun"
};
const size_t fang_spawn_noun_count =
    sizeof(fang_spawn_nouns) / sizeof(fang_spawn_nouns[0]);

static char fang_ascii_lower(char character) {
    if (character >= 'A' && character <= 'Z') {
        return (char)(character + ('a' - 'A'));
    }
    return character;
}

static int fang_warp_name_equals(
    const char* location, const char* partial, size_t partial_length) {
    size_t location_length = strlen(location);
    size_t index;
    if (location_length != partial_length) {
        return 0;
    }
    for (index = 0; index < partial_length; index++) {
        if (fang_ascii_lower(location[index]) != fang_ascii_lower(partial[index])) {
            return 0;
        }
    }
    return 1;
}

int fang_warp_name_contains(
    const char* location, const char* partial, size_t partial_length) {
    size_t location_length = strlen(location);
    size_t start;
    size_t index;
    if (partial_length == 0 || partial_length > location_length) {
        return 0;
    }
    for (start = 0; start + partial_length <= location_length; start++) {
        for (index = 0; index < partial_length; index++) {
            if (fang_ascii_lower(location[start + index]) !=
                fang_ascii_lower(partial[index])) {
                break;
            }
        }
        if (index == partial_length) {
            return 1;
        }
    }
    return 0;
}

static const char* find_fang_warp_alias(const char* partial, size_t partial_length) {
    unsigned int alias = 0;
    size_t index;
    size_t alias_count = sizeof(fang_warp_aliases) / sizeof(fang_warp_aliases[0]);
    if (partial_length == 0) {
        return NULL;
    }
    for (index = 0; index < partial_length; index++) {
        unsigned int digit;
        if (partial[index] < '0' || partial[index] > '9') {
            return NULL;
        }
        digit = (unsigned int)(partial[index] - '0');
        if (alias > 1000) {
            return NULL;
        }
        alias = alias * 10 + digit;
    }
    if (alias == 0 || alias > alias_count) {
        return NULL;
    }
    return fang_warp_aliases[alias - 1];
}

const char* find_fang_warp_location(const char* partial, size_t partial_length) {
    const char* matched_location = find_fang_warp_alias(partial, partial_length);
    size_t location_index;
    unsigned int match_count = 0;
    if (matched_location != NULL) {
        return matched_location;
    }
    for (location_index = 0;
        location_index < sizeof(fang_warp_locations) / sizeof(fang_warp_locations[0]);
        location_index++) {
        const char* location = fang_warp_locations[location_index];
        if (fang_warp_name_equals(location, partial, partial_length)) {
            return location;
        }
        if (!fang_warp_name_contains(location, partial, partial_length)) {
            continue;
        }
        matched_location = location;
        match_count++;
    }
    if (match_count != 1) {
        return NULL;
    }
    return matched_location;
}

static int fang_spawn_name_equals(
    const char* noun_name, const char* partial, size_t partial_length) {
    size_t noun_length = strlen(noun_name);
    size_t index;
    if (fang_warp_name_equals(noun_name, partial, partial_length)) {
        return 1;
    }
    if (noun_length <= 5 || partial_length != noun_length - 5 ||
        fang_ascii_lower(noun_name[noun_length - 5]) != '.' ||
        fang_ascii_lower(noun_name[noun_length - 4]) != 'n' ||
        fang_ascii_lower(noun_name[noun_length - 3]) != 'o' ||
        fang_ascii_lower(noun_name[noun_length - 2]) != 'u' ||
        fang_ascii_lower(noun_name[noun_length - 1]) != 'n') {
        return 0;
    }
    for (index = 0; index < partial_length; index++) {
        if (fang_ascii_lower(noun_name[index]) != fang_ascii_lower(partial[index])) {
            return 0;
        }
    }
    return 1;
}

const char* find_fang_spawn_noun(const char* partial, size_t partial_length) {
    const char* matched_noun = NULL;
    size_t noun_index;
    unsigned int match_count = 0;
    for (noun_index = 0;
        noun_index < sizeof(fang_spawn_nouns) / sizeof(fang_spawn_nouns[0]);
        noun_index++) {
        const char* noun_name = fang_spawn_nouns[noun_index];
        if (fang_spawn_name_equals(noun_name, partial, partial_length)) {
            return noun_name;
        }
        if (!fang_warp_name_contains(noun_name, partial, partial_length)) {
            continue;
        }
        matched_noun = noun_name;
        match_count++;
    }
    if (match_count != 1) {
        return NULL;
    }
    return matched_noun;
}

/* Darkspin chat commands that carry a session secret count only as the first
   word, which is the server's own command rule. */
int is_first_chat_token(const char* text, const char* command_text) {
    const char* cursor = text;
    if (text == NULL || command_text == NULL || command_text < text) {
        return 0;
    }
    while (cursor < command_text && (*cursor == ' ' || *cursor == '\t')) {
        cursor++;
    }
    return cursor == command_text;
}
