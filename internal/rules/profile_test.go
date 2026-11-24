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

func TestProfileContactAliases(t *testing.T) {
	for _, tc := range []struct {
		bio  string
		want bool
	}{
		{"黑鹰广告宣传联系：@heiying666", true},
		{"BuI黑鹰广告合作联系：@heiying666 N3g", true},
		{"黑鹰广告联系 @HEIYING666", true},
		{"黑 鷹 廣 告 合 作 聯 繫：＠ｈｅｉｙｉｎｇ６６６", true},
		{"黑鹰广告推广联系：https://t.me/heiying666", true},
		{"黑鹰广告合作联系：@hei\u200bying666", true},
		{"黑鹰", false}, {"黑鹰广告", false}, {"@heiying666", false},
		{"黑鹰广告合作联系：@heiying6667", false},
		{"黑鹰广告合作联系：@heiying666_other", false},
		{"黑鹰广告合作联系：@otheruser", false},
		{"黑鹰广告合作联系：文字介绍 @heiying666", false},
		{"黑鹰广告合作联系：https://evil.t.me/heiying666", false},
		{"研究黑鹰鸟类，联系 @heiying666", false},
	} {
		d, reason := ProfileMatch(tc.bio, []string{"heiying.co"})
		if (d != "") != tc.want || tc.want && reason != "profile_contact:heiying666" {
			t.Errorf("%q: %q %q", tc.bio, d, reason)
		}
		if d, _ := ProfileMatch(tc.bio, []string{"other.example"}); d != "" {
			t.Errorf("unconfigured alias matched: %q", tc.bio)
		}
	}
	if d, reason := ProfileMatch("黑鹰广告联系 @heiying666 heiying.co", []string{"heiying.co"}); d != "heiying.co" || reason != "profile_domain:heiying.co" {
		t.Fatal(d, reason)
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
