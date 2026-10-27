package streamcontract

import "testing"

func TestRenditionPolicy(t *testing.T) {
	for _, tc := range []struct {
		codec, profile string
		valid          bool
	}{{"vp8", "", true}, {"vp9", "0", true}, {"h264", "baseline", true}, {"av1", "main", true}, {"vp9", "2", false}, {"av1", "main10", false}, {"h264", "high", false}, {"unknown", "", false}} {
		v := Rendition{ID: "sdr", Codec: tc.codec, Profile: tc.profile, DynamicRange: "sdr", BitDepth: 8, ColorPrimaries: "bt709", Transfer: "bt709", Matrix: "bt709"}
		if ValidRenditions([]Rendition{v}) != tc.valid {
			t.Fatalf("incorrect SDR policy for %s/%s", tc.codec, tc.profile)
		}
	}
	sdr := Rendition{ID: "sdr", Codec: "vp8", DynamicRange: "sdr", BitDepth: 8, ColorPrimaries: "bt709", Transfer: "bt709", Matrix: "bt709"}
	for _, dynamicRange := range []string{"hdr10", "hlg"} {
		for _, codec := range []string{"vp9", "av1"} {
			profile, transfer := "2", "pq"
			if codec == "av1" {
				profile = "main10"
			}
			if dynamicRange == "hlg" {
				transfer = "hlg"
			}
			hdr := Rendition{ID: "hdr", Codec: codec, Profile: profile, DynamicRange: dynamicRange, BitDepth: 10, ColorPrimaries: "bt2020", Transfer: transfer, Matrix: "bt2020-ncl"}
			if !ValidRenditions([]Rendition{sdr, hdr}) {
				t.Fatal("valid fallback rejected")
			}
			if ValidRenditions([]Rendition{hdr}) {
				t.Fatal("HDR-only accepted")
			}
			hdr.ID = sdr.ID
			if ValidRenditions([]Rendition{sdr, hdr}) {
				t.Fatal("duplicate ID accepted")
			}
		}
	}
	if ValidRenditions([]Rendition{sdr, sdr}) {
		t.Fatal("duplicate range accepted")
	}
	for _, id := range []string{"", "Uppercase", "../../secret", "space id"} {
		v := sdr
		v.ID = id
		if ValidRenditions([]Rendition{v}) {
			t.Fatal("invalid ID accepted", id)
		}
	}
}
