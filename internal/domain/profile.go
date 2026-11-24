package domain

import (
	"errors"
	"regexp"
)

var profileDomain = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$`)

func validateProfileDomains(domains []string) error {
	if len(domains) > 20 {
		return errors.New("简介广告域名最多 20 个")
	}
	for _, d := range domains {
		if len(d) > 253 || !profileDomain.MatchString(d) {
			return errors.New("简介广告域名须使用小写域名，不含协议、路径或空格")
		}
	}
	return nil
}
