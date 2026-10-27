package model

import "encoding/json"

// ReplayObjectIsCopy recovers copy identity from legacy replay details. Missing
// details are not evidence of a copy; callers retain any previously known flag.
func ReplayObjectIsCopy(details string) bool {
	var object struct {
		IsCopy bool `json:"isCopy"`
	}
	_ = json.Unmarshal([]byte(details), &object)
	return object.IsCopy
}

// ReplayCastInstances uses completed CastSpell transfers, not the Actions list:
// Arena's Actions describe available choices, including spells never cast.
func ReplayCastInstances(annotations string) map[int64]bool {
	var payload struct {
		Annotations []struct {
			Type        []string `json:"type"`
			AffectedIDs []int64  `json:"affectedIds"`
			Details     []struct {
				Key         string   `json:"key"`
				ValueString []string `json:"valueString"`
			} `json:"details"`
		} `json:"annotations"`
	}
	_ = json.Unmarshal([]byte(annotations), &payload)
	casts := make(map[int64]bool)
	for _, annotation := range payload.Annotations {
		transfer := false
		for _, kind := range annotation.Type {
			transfer = transfer || kind == "AnnotationType_ZoneTransfer"
		}
		if !transfer {
			continue
		}
		for _, detail := range annotation.Details {
			if detail.Key == "category" && len(detail.ValueString) > 0 && detail.ValueString[0] == "CastSpell" {
				for _, id := range annotation.AffectedIDs {
					casts[id] = true
				}
			}
		}
	}
	return casts
}

// PopulateReplayCopyFlags also repairs legacy archives at read time. Copy
// identity survives sparse object diffs and applies only within the same game.
func PopulateReplayCopyFlags(frames []MatchReplayFrameRow) {
	type key struct{ game, instance int64 }
	copies, casts := make(map[key]bool), make(map[key]bool)
	for _, frame := range frames {
		game := int64(1)
		if frame.GameNumber != nil {
			game = *frame.GameNumber
		}
		for id := range ReplayCastInstances(frame.AnnotationsJSON) {
			casts[key{game, id}] = true
		}
		for _, object := range frame.Objects {
			k := key{game, object.InstanceID}
			copies[k] = copies[k] || object.IsCopy || ReplayObjectIsCopy(object.DetailsJSON)
			casts[k] = casts[k] || object.CastConfirmed
		}
	}
	for i := range frames {
		game := int64(1)
		if frames[i].GameNumber != nil {
			game = *frames[i].GameNumber
		}
		for j := range frames[i].Objects {
			object := &frames[i].Objects[j]
			k := key{game, object.InstanceID}
			object.IsCopy = copies[k]
			object.CastConfirmed = casts[k]
		}
	}
}
