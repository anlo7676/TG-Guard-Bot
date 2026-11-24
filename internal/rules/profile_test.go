package rules

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestProfileDomainBoundaries(t *testing.T) {
	for _, tc := range []struct {
		bio  string
		want bool
	}{
		{"黑鹰广告官方网站： heiying.co", true}, {"HTTPS://WWW.HEIYING.CO/ad", true},
		{"ｈｅｉｙｉｎｇ．ｃｏ", true}, {"hei\u200bying.co", true},
		{"notheiying.co", false}, {"heiying.co.evil.com", false}, {"heiying.com", false},
		{"heiying.co_foo", false}, {"heiying.co中文", false}, {"", false},
	} {
		if got := ProfileDomain(tc.bio, []string{"heiying.co"}) != ""; got != tc.want {
			t.Errorf("%q got %v", tc.bio, got)
		}
	}
}

func TestAvatarHashUsesPixelsAndRejectsInvalidFiles(t *testing.T) {
	im := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	im.Set(2, 3, color.NRGBA{R: 200, A: 255})
	encode := func(level png.CompressionLevel) []byte {
		var b bytes.Buffer
		enc := png.Encoder{CompressionLevel: level}
		if err := enc.Encode(&b, im); err != nil {
			t.Fatal(err)
		}
		return b.Bytes()
	}
	a, err := AvatarHash(encode(png.NoCompression))
	if err != nil {
		t.Fatal(err)
	}
	b, err := AvatarHash(encode(png.BestCompression))
	if err != nil || a != b {
		t.Fatal("same pixels did not match", err)
	}
	im.Set(2, 3, color.NRGBA{R: 201, A: 255})
	c, _ := AvatarHash(encode(png.DefaultCompression))
	if c == a {
		t.Fatal("different pixels matched")
	}
	for _, data := range [][]byte{nil, []byte("invalid"), make([]byte, (2<<20)+1)} {
		if _, err := AvatarHash(data); err == nil {
			t.Fatal("accepted invalid image")
		}
	}
}
