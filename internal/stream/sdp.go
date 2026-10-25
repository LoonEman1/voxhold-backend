package stream

import (
	"strconv"
	"strings"
)

// withVideoBandwidth caps every video m-section with b=AS (kbit/s). Browsers
// apply a remote b=AS as the ceiling of their encoder, so this keeps the
// publisher inside the SFU ingest limit. Existing bandwidth lines of video
// sections are replaced; other sections are left untouched.
func withVideoBandwidth(sdp string, kbps int) string {
	if kbps <= 0 || sdp == "" {
		return sdp
	}
	eol := "\r\n"
	if !strings.Contains(sdp, eol) {
		eol = "\n"
	}
	lines := strings.Split(sdp, eol)
	result := make([]string, 0, len(lines)+4)
	bandwidth := "b=AS:" + strconv.Itoa(kbps)
	inVideo := false
	inserted := false
	for index, line := range lines {
		if strings.HasPrefix(line, "m=") {
			inVideo = strings.HasPrefix(line, "m=video ")
			inserted = false
			result = append(result, line)
			continue
		}
		if inVideo {
			if strings.HasPrefix(line, "b=") {
				continue
			}
			// RFC 8866 order inside a media description: i=, c=, b=, k=, a=.
			// The trailing empty element after the final EOL also closes the
			// section.
			last := index == len(lines)-1 && line == ""
			if !inserted && (last || (!strings.HasPrefix(line, "i=") && !strings.HasPrefix(line, "c="))) {
				result = append(result, bandwidth)
				inserted = true
			}
		}
		result = append(result, line)
	}
	return strings.Join(result, eol)
}
