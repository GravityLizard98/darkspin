package game

import "math"

// A1C3F0 adds the offset in world space and passes half box dimensions and
// the marker orientation to physics. This evaluates hero centers; native
// physics shape/footprint contact inflation remains a separate parity limit.
func campaignTriggerContains(trigger CampaignDirectorTrigger, position Vec3) bool {
	volume := trigger.SpawnTrigger.TriggerVolume
	local := campaignTriggerLocalPosition(trigger, position)
	if volume.Shape == uint32(TriggerSphere) {
		return campaignDistanceSquared(local, Vec3{}) <= volume.SphereRadius*volume.SphereRadius
	}
	if volume.Shape == uint32(TriggerBox) {
		return math.Abs(float64(local.X)) <= float64(volume.BoxDimensions.X)/2 &&
			math.Abs(float64(local.Y)) <= float64(volume.BoxDimensions.Y)/2 &&
			math.Abs(float64(local.Z)) <= float64(volume.BoxDimensions.Z)/2
	}
	// No campaign spawn trigger in the audited corpus uses a capsule.
	return false
}

func campaignTriggerLocalPosition(trigger CampaignDirectorTrigger, position Vec3) Vec3 {
	volume := trigger.SpawnTrigger.TriggerVolume
	local := Vec3{
		X: position.X - trigger.Position.X - volume.Offset.X,
		Y: position.Y - trigger.Position.Y - volume.Offset.Y,
		Z: position.Z - trigger.Position.Z - volume.Offset.Z,
	}
	if volume.Shape == uint32(TriggerSphere) {
		return local
	}
	// Inverse of the authored degree Euler rotation (Rz * Ry * Rx).
	sx, cx := math.Sincos(float64(trigger.Rotation.X) * math.Pi / 180)
	sy, cy := math.Sincos(float64(trigger.Rotation.Y) * math.Pi / 180)
	sz, cz := math.Sincos(float64(trigger.Rotation.Z) * math.Pi / 180)
	x := float64(local.X)*cz + float64(local.Y)*sz
	y := -float64(local.X)*sz + float64(local.Y)*cz
	z := float64(local.Z)
	x, z = x*cy-z*sy, x*sy+z*cy
	y, z = y*cx+z*sx, -y*sx+z*cx
	return Vec3{X: float32(x), Y: float32(y), Z: float32(z)}
}

// Preserve zero-delay entry when an accepted movement crosses a thin volume.
// Positive dwell requires sampled occupancy; a segment alone proves no dwell.
func campaignTriggerCrossed(trigger CampaignDirectorTrigger, previous Vec3, current Vec3) bool {
	volume := trigger.SpawnTrigger.TriggerVolume
	start := campaignTriggerLocalPosition(trigger, previous)
	end := campaignTriggerLocalPosition(trigger, current)
	if volume.Shape == uint32(TriggerSphere) {
		return campaignSegmentDistanceSquared(start, end, Vec3{}) <= volume.SphereRadius*volume.SphereRadius
	}
	if volume.Shape != uint32(TriggerBox) {
		return false
	}
	minimum, maximum := float32(0), float32(1)
	components := [][3]float32{
		{start.X, end.X, volume.BoxDimensions.X / 2},
		{start.Y, end.Y, volume.BoxDimensions.Y / 2},
		{start.Z, end.Z, volume.BoxDimensions.Z / 2},
	}
	for _, axis := range components {
		delta := axis[1] - axis[0]
		if delta == 0 {
			if axis[0] < -axis[2] || axis[0] > axis[2] {
				return false
			}
			continue
		}
		first, last := (-axis[2]-axis[0])/delta, (axis[2]-axis[0])/delta
		minimum = max(minimum, min(first, last))
		maximum = min(maximum, max(first, last))
		if minimum > maximum {
			return false
		}
	}
	return true
}
