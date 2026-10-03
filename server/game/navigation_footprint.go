package game

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/darkspinnet/darkspin/server/util"
)

// NavigationFootprint preserves noun size-class geometry separately from
// combat/physics footprints and placement collision boxes.
type NavigationFootprint struct {
	SizeClass uint32
	Extents   Vec3
	Minimum   Vec3
	Maximum   Vec3
}

func (e NavigationFootprint) SpawnRadius() (float32, error) {
	if e.SizeClass > 17 {
		return 0, errors.New("unsupported navigation size class")
	}
	if e.SizeClass != 0 {
		if !isNavigationScalar(e.Extents.X) || e.Extents.X <= 0 {
			return 0, errors.New("navigation extents unavailable")
		}
		return e.Extents.X * 0.5, nil
	}
	if !isNavigationScalar(e.Minimum.X) || !isNavigationScalar(e.Minimum.Y) ||
		!isNavigationScalar(e.Maximum.X) || !isNavigationScalar(e.Maximum.Y) ||
		e.Minimum.X > e.Maximum.X || e.Minimum.Y > e.Maximum.Y {
		return 0, errors.New("invalid navigation bounds")
	}
	x := max(math.Abs(float64(e.Minimum.X)), math.Abs(float64(e.Maximum.X)))
	y := max(math.Abs(float64(e.Minimum.Y)), math.Abs(float64(e.Maximum.Y)))
	return float32(math.Sqrt(x*x + y*y)), nil
}

func (e NavigationFootprint) ActorRadius(scale float32) (float32, error) {
	if !isNavigationScalar(scale) || scale <= 0 {
		return 0, errors.New("invalid navigation scale")
	}
	radius, err := e.SpawnRadius()
	if err != nil {
		return 0, fmt.Errorf("actorRadius: %w", err)
	}
	if e.SizeClass != 0 {
		x := float64(e.Extents.X)
		radius = float32(0.5 * float64(scale) * math.Sqrt(x*x+x*x))
	} else {
		x := max(math.Abs(float64(e.Minimum.X)), math.Abs(float64(e.Maximum.X)))
		y := max(math.Abs(float64(e.Minimum.Y)), math.Abs(float64(e.Maximum.Y)))
		radius = float32(float64(scale) * math.Sqrt(x*x+y*y))
	}
	if !isNavigationScalar(radius) {
		return 0, errors.New("navigation radius overflow")
	}
	return radius, nil
}

func (e CampaignDirector) NavigationFootprintForAsset(nounName string) (NavigationFootprint, bool) {
	stem := strings.TrimSuffix(strings.ToLower(nounName), ".noun")
	footprint, isFound := e.NounFootprintsByInstance[util.HashID(stem)]
	return footprint, isFound
}

func isNavigationScalar(scalar float32) bool {
	return !math.IsNaN(float64(scalar)) && !math.IsInf(float64(scalar), 0)
}
