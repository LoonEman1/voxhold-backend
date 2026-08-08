package profile

import (
	"errors"
	"strings"
	"unicode/utf8"
)

const isoCountryCodes = "" +
	"|AD|AE|AF|AG|AI|AL|AM|AO|AQ|AR|AS|AT|AU|AW|AX|AZ" +
	"|BA|BB|BD|BE|BF|BG|BH|BI|BJ|BL|BM|BN|BO|BQ|BR|BS|BT|BV|BW|BY|BZ" +
	"|CA|CC|CD|CF|CG|CH|CI|CK|CL|CM|CN|CO|CR|CU|CV|CW|CX|CY|CZ" +
	"|DE|DJ|DK|DM|DO|DZ" +
	"|EC|EE|EG|EH|ER|ES|ET" +
	"|FI|FJ|FK|FM|FO|FR" +
	"|GA|GB|GD|GE|GF|GG|GH|GI|GL|GM|GN|GP|GQ|GR|GS|GT|GU|GW|GY" +
	"|HK|HM|HN|HR|HT|HU" +
	"|ID|IE|IL|IM|IN|IO|IQ|IR|IS|IT" +
	"|JE|JM|JO|JP" +
	"|KE|KG|KH|KI|KM|KN|KP|KR|KW|KY|KZ" +
	"|LA|LB|LC|LI|LK|LR|LS|LT|LU|LV|LY" +
	"|MA|MC|MD|ME|MF|MG|MH|MK|ML|MM|MN|MO|MP|MQ|MR|MS|MT|MU|MV|MW|MX|MY|MZ" +
	"|NA|NC|NE|NF|NG|NI|NL|NO|NP|NR|NU|NZ" +
	"|OM" +
	"|PA|PE|PF|PG|PH|PK|PL|PM|PN|PR|PS|PT|PW|PY" +
	"|QA" +
	"|RE|RO|RS|RU|RW" +
	"|SA|SB|SC|SD|SE|SG|SH|SI|SJ|SK|SL|SM|SN|SO|SR|SS|ST|SV|SX|SY|SZ" +
	"|TC|TD|TF|TG|TH|TJ|TK|TL|TM|TN|TO|TR|TT|TV|TW|TZ" +
	"|UA|UG|UM|US|UY|UZ" +
	"|VA|VC|VE|VG|VI|VN|VU" +
	"|WF|WS" +
	"|YE|YT" +
	"|ZA|ZM|ZW|"

var (
	ErrNothingToUpdate = errors.New(
		"at least one profile field is required",
	)

	ErrAboutTooLong = errors.New(
		"about must not exceed 512 characters",
	)

	ErrCountryCodeInvalid = errors.New(
		"country code must contain two Latin letters",
	)
)

type UpdateInput struct {
	About       *string
	CountryCode *string
}

func (i UpdateInput) Normalize() UpdateInput {
	if i.About != nil {
		about := strings.TrimSpace(*i.About)
		i.About = &about
	}

	if i.CountryCode != nil {
		countryCode := strings.ToUpper(
			strings.TrimSpace(*i.CountryCode),
		)

		i.CountryCode = &countryCode
	}

	return i
}

func (i UpdateInput) Validate() error {
	if i.About == nil && i.CountryCode == nil {
		return ErrNothingToUpdate
	}

	if i.About != nil &&
		utf8.RuneCountInString(*i.About) > 512 {
		return ErrAboutTooLong
	}

	if i.CountryCode != nil {
		countryCode := *i.CountryCode

		if countryCode != "" &&
			!isCountryCodeValid(countryCode) {
			return ErrCountryCodeInvalid
		}
	}

	return nil
}

func isCountryCodeValid(value string) bool {
	if len(value) != 2 {
		return false
	}

	return strings.Contains(
		isoCountryCodes,
		"|"+value+"|",
	)
}
