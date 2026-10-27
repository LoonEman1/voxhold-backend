// Package streamcontract defines the shared signaling and SFU rendition policy.
package streamcontract

type Rendition struct {
	ID, Codec, Profile, DynamicRange string
	BitDepth                         int
	ColorPrimaries, Transfer, Matrix string
}

func ValidID(id string) bool {
	if len(id) == 0 || len(id) > 32 {
		return false
	}
	for _, c := range id {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' && c != '_' {
			return false
		}
	}
	return true
}

func ValidRenditions(renditions []Rendition) bool {
	if len(renditions) == 0 || len(renditions) > 2 {
		return false
	}
	ids := make(map[string]struct{}, len(renditions))
	ranges := make(map[string]struct{}, len(renditions))
	hasSDR := false
	for _, value := range renditions {
		if !ValidID(value.ID) || value.BitDepth == 0 {
			return false
		}
		if _, duplicate := ids[value.ID]; duplicate {
			return false
		}
		if _, duplicate := ranges[value.DynamicRange]; duplicate {
			return false
		}
		ids[value.ID] = struct{}{}
		ranges[value.DynamicRange] = struct{}{}
		switch value.DynamicRange {
		case "sdr":
			hasSDR = value.BitDepth == 8 && value.ColorPrimaries == "bt709" &&
				value.Transfer == "bt709" && value.Matrix == "bt709" &&
				validSDRProfile(value.Codec, value.Profile)
			if !hasSDR {
				return false
			}
		case "hdr10":
			if value.BitDepth != 10 || value.ColorPrimaries != "bt2020" ||
				value.Transfer != "pq" || value.Matrix != "bt2020-ncl" ||
				!validHDRProfile(value.Codec, value.Profile) {

				return false
			}
		case "hlg":
			if value.BitDepth != 10 || value.ColorPrimaries != "bt2020" ||
				value.Transfer != "hlg" || value.Matrix != "bt2020-ncl" ||
				!validHDRProfile(value.Codec, value.Profile) {

				return false
			}
		default:
			return false
		}
	}
	return hasSDR
}

func validSDRProfile(codec string, profile string) bool {
	return (codec == "vp8" && profile == "") ||
		(codec == "vp9" && profile == "0") ||
		(codec == "h264" && profile == "baseline") ||
		(codec == "av1" && profile == "main")
}

func validHDRProfile(codec string, profile string) bool {
	return (codec == "vp9" && profile == "2") ||
		(codec == "av1" && profile == "main10")
}
