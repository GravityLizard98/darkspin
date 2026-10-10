package sqlite

import (
	"context"
	"errors"
	"fmt"
)

// LocalizedTexts reads one imported localization table of locale, keyed by its
// lowercase "0x%08x" locale key. A database without imported text returns an
// empty map.
func (e *Store) LocalizedTexts(
	ctx context.Context, locale string, tableID uint32,
) (map[string]string, error) {
	if e == nil || e.database == nil || ctx == nil {
		return nil, errors.New("localized text store or context unavailable")
	}
	isLocalizationStored, err := hasTable(ctx, e.database, "localization_text")
	if err != nil {
		return nil, fmt.Errorf("localeTable: %w", err)
	}
	textsByKey := make(map[string]string)
	if !isLocalizationStored {
		return textsByKey, nil
	}
	rows, err := e.database.QueryContext(ctx, `
		SELECT locale_key, localized_text
		FROM localization_text
		WHERE locale = ? AND table_id = ?`, locale, tableID)
	if err != nil {
		return nil, fmt.Errorf("localeTextQuery: %w", err)
	}
	for rows.Next() {
		var localeKey string
		var localizedText string
		err = rows.Scan(&localeKey, &localizedText)
		if err != nil {
			closeErr := rows.Close()
			return nil, fmt.Errorf("localeTextScan: %w", errors.Join(err, closeErr))
		}
		textsByKey[localeKey] = localizedText
	}
	err = rows.Err()
	closeErr := rows.Close()
	if err != nil {
		return nil, fmt.Errorf("localeTextRows: %w", errors.Join(err, closeErr))
	}
	if closeErr != nil {
		return nil, fmt.Errorf("localeTextClose: %w", closeErr)
	}
	return textsByKey, nil
}
