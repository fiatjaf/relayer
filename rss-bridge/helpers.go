package main

import (
	"net/url"
	"path"
)

func urljoin(baseUrl string, elem ...string) (result string, err error) {
	u, err := url.Parse(baseUrl)
	if err != nil {
		return
	}

	if len(elem) > 0 {
		elem = append([]string{u.Path}, elem...)
		u.Path = path.Join(elem...)
	}

	return u.String(), nil
}
