package rules

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"regexp"
	"strings"
)

// Consume whole hostname-shaped tokens so lookalike suffixes cannot match.
var profileHost = regexp.MustCompile(`[\pL\pN_-]+(?:\.[\pL\pN_-]+)+`)

// A verified advertiser alias requires both the business claim and its exact
// contact, next to each other. Random prefix/suffix text is not an exemption.
var heiyingContact = regexp.MustCompile(`黑\s*[鹰鷹]\s*[广廣]\s*告\s*(?:宣\s*[传傳]|合\s*作|推\s*[广廣]|商\s*[务務])?\s*[联聯]\s*[系繫][\s:：|｜,，、/／-]*(?:@|(?:https?://)?(?:t|telegram)\.me/)heiying666(?:$|[^a-z0-9_])`)

// ProfileMatch associates verified contact aliases with the configured domain.
// Removing heiying.co disables both its domain and contact-phrase matching.
func ProfileMatch(bio string, domains []string) (domain, reason string) {
	if d := ProfileDomain(bio, domains); d != "" {
		return d, "profile_domain:" + d
	}
	for _, d := range domains {
		if d == "heiying.co" && heiyingContact.MatchString(Clean(bio)) {
			return d, "profile_contact:heiying666"
		}
	}
	return "", ""
}

func ProfileDomain(bio string, domains []string) string {
	for _, host := range profileHost.FindAllString(Clean(bio), -1) {
		for _, d := range domains {
			if host == d || strings.HasSuffix(host, "."+d) {
				return d
			}
		}
	}
	return ""
}

// AvatarHash matches exact decoded pixels, not similar faces, logos or colours.
// Re-encoding that changes pixels intentionally does not produce a match.
func AvatarHash(data []byte) (string, error) {
	if len(data) == 0 || len(data) > 2<<20 {
		return "", errors.New("avatar size unsupported")
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 2048 || cfg.Height > 2048 {
		return "", errors.New("avatar dimensions unsupported")
	}
	im, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", errors.New("avatar decoding failed")
	}
	h := sha256.New()
	h.Write([]byte("rgba-avatar-v1"))
	_ = binary.Write(h, binary.BigEndian, uint32(cfg.Width))
	_ = binary.Write(h, binary.BigEndian, uint32(cfg.Height))
	row := make([]byte, cfg.Width*4)
	for y := 0; y < cfg.Height; y++ {
		for x := 0; x < cfg.Width; x++ {
			c := color.NRGBAModel.Convert(im.At(im.Bounds().Min.X+x, im.Bounds().Min.Y+y)).(color.NRGBA)
			copy(row[x*4:], []byte{c.R, c.G, c.B, c.A})
		}
		h.Write(row)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
