package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
)

func (e campaignPreparation) selectInitialMapLayout(
	director game.CampaignDirector, binding game.GameplayBinding,
) (game.CampaignDirector, error) {
	// Use the same adapter inputs as the prepare packet, including its native
	// condition mask and seed sentinel handling. Never roll from the game ID.
	message, err := zonePrepareMessage(binding)
	if err != nil {
		return game.CampaignDirector{}, fmt.Errorf("layoutPrepare: %w", err)
	}
	ordinals, err := director.SelectMapLayout(message.VariantSeed, message.ConditionMask)
	if err != nil {
		return game.CampaignDirector{}, fmt.Errorf("layoutSelect: %w", err)
	}
	selectedDirector, err := director.InitialMapLayout(message.VariantSeed, ordinals)
	if err != nil {
		return game.CampaignDirector{}, fmt.Errorf("layoutProject: %w", err)
	}
	if e.logger != nil {
		setsByOrdinal := make(map[int]game.CampaignDirectorMarkerSet, len(director.MarkerSets))
		for _, set := range director.MarkerSets {
			setsByOrdinal[set.Ordinal] = set
		}
		selectedAssets := make([]string, 0, len(ordinals))
		for _, ordinal := range ordinals {
			set := setsByOrdinal[ordinal]
			selectedAssets = append(selectedAssets, mapLayoutAssetLabel(set.Name, set.CatalogAsset))
		}
		e.logger.Printf(
			"RakNet map layout game=%d level=%q stage=%d seed=%#x conditions=%#x level_catalog=%s selected_sets=%v",
			binding.GameID, director.Level, message.LevelIndex,
			message.VariantSeed, message.ConditionMask,
			mapLayoutAssetLabel(director.Level, director.LevelCatalogAsset), selectedAssets,
		)
	}
	return selectedDirector, nil
}

func mapLayoutAssetLabel(reference string, asset game.CampaignAssetIdentity) string {
	if asset.Ordinal == nil {
		return fmt.Sprintf("%q catalog=unresolved", reference)
	}
	return fmt.Sprintf("%q catalog=%d asset=%q source=%q", reference,
		*asset.Ordinal, asset.AssetName, asset.SourceName)
}
