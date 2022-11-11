package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/nbd-wtf/go-nostr"
	strip "github.com/grokify/html-strip-tags-go"
	"github.com/mmcdole/gofeed"
	"github.com/rif/cache2go"
)

var (
	fp        = gofeed.NewParser()
	feedCache = cache2go.New(512, time.Minute*19)
	client    = &http.Client{
		Timeout: 5 * time.Second,
	}
)

type Entity struct {
	PrivateKey string
	URL        string
}

var types = []string{
	"rss+xml",
	"atom+xml",
	"feed+json",
	"text/xml",
	"application/xml",
}

func getFeedURL(url string) string {
	resp, err := client.Get(url)
	if err != nil || resp.StatusCode >= 300 {
		return ""
	}

	ct := resp.Header.Get("Content-Type")
	for _, typ := range types {
		if strings.Contains(ct, typ) {
			return url
		}
	}

	if strings.Contains(ct, "text/html") {
		doc, err := goquery.NewDocumentFromReader(resp.Body)
		if err != nil {
			return ""
		}

		for _, typ := range types {
			href, _ := doc.Find(fmt.Sprintf("link[type*='%s']", typ)).Attr("href")
			if href == "" {
				continue
			}
			if !strings.HasPrefix(href, "http") {
				href, _ = urljoin(url, href)
			}
			return href
		}
	}

	return ""
}

func parseFeed(url string) (*gofeed.Feed, error) {
	if feed, ok := feedCache.Get(url); ok {
		return feed.(*gofeed.Feed), nil
	}

	feed, err := fp.ParseURL(url)
	if err != nil {
		return nil, err
	}

	// cleanup a little so we don't store too much junk
	for i := range feed.Items {
		feed.Items[i].Content = ""
	}
	feedCache.Set(url, feed)

	return feed, nil
}

func feedToSetMetadata(pubkey string, feed *gofeed.Feed) nostr.Event {
	metadata := map[string]string{
		"name":  feed.Title,
		"about": feed.Description + "\n\n" + feed.Link,
	}
	if feed.Image != nil {
		metadata["picture"] = feed.Image.URL
	}
	content, _ := json.Marshal(metadata)

	createdAt := time.Now()
	if feed.PublishedParsed != nil {
		createdAt = *feed.PublishedParsed
	}

	evt := nostr.Event{
		PubKey:    pubkey,
		CreatedAt: createdAt,
		Kind:      nostr.KindSetMetadata,
		Tags:      nostr.Tags{},
		Content:   string(content),
	}
	evt.ID = string(evt.Serialize())

	return evt
}

func itemToTextNote(pubkey string, item *gofeed.Item) nostr.Event {
	content := ""
	if item.Title != "" {
		content = "**" + item.Title + "**\n\n"
	}
	content += strip.StripTags(item.Description)
	if len(content) > 250 {
		content += content[0:249] + "…"
	}
	content += "\n\n" + item.Link

	createdAt := time.Now()
	if item.UpdatedParsed != nil {
		createdAt = *item.UpdatedParsed
	}
	if item.PublishedParsed != nil {
		createdAt = *item.PublishedParsed
	}

	evt := nostr.Event{
		PubKey:    pubkey,
		CreatedAt: createdAt,
		Kind:      nostr.KindTextNote,
		Tags:      nostr.Tags{},
		Content:   content,
	}
	evt.ID = string(evt.Serialize())

	return evt
}

func privateKeyFromFeed(url string) string {
	m := hmac.New(sha256.New, []byte(relay.Secret))
	m.Write([]byte(url))
	r := m.Sum(nil)
	return hex.EncodeToString(r)
}
