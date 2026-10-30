package linkpreview

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
)

type Preview struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Site        string `json:"site_name"`
	ImagePath   string `json:"image_path,omitempty"`
}

func plain(value string, limit int) string {
	value = strings.Join(strings.FieldsFunc(value, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }), " ")
	runes := []rune(value)
	if len(runes) > limit {
		value = string(runes[:limit])
	}
	return value
}
func parseHTML(data []byte, contentType string, base *url.URL) (Preview, string, error) {
	reader, err := charset.NewReader(bytes.NewReader(data), contentType)
	if err != nil {
		return Preview{}, "", err
	}
	tokenizer := html.NewTokenizer(reader)
	meta := map[string]string{}
	title := ""
	inTitle := false
	for {
		t := tokenizer.Next()
		if t == html.ErrorToken {
			break
		}
		if t == html.StartTagToken || t == html.SelfClosingTagToken {
			token := tokenizer.Token()
			if token.Data == "body" {
				break
			}
			if token.Data == "title" {
				inTitle = true
			}
			if token.Data != "meta" {
				continue
			}
			key, value := "", ""
			for _, a := range token.Attr {
				if a.Key == "property" || a.Key == "name" {
					key = strings.ToLower(a.Val)
				}
				if a.Key == "content" {
					value = a.Val
				}
			}
			if meta[key] == "" {
				meta[key] = value
			}
		} else if t == html.EndTagToken {
			if tokenizer.Token().Data == "title" {
				inTitle = false
			}
		} else if t == html.TextToken && inTitle {
			title += string(tokenizer.Text())
		}
	}
	first := func(keys ...string) string {
		for _, key := range keys {
			if v := strings.TrimSpace(meta[key]); v != "" {
				return v
			}
		}
		return ""
	}
	if value := first("og:title", "twitter:title"); value != "" {
		title = value
	}
	p := Preview{URL: base.String(), Title: plain(title, 300), Description: plain(first("og:description", "twitter:description", "description"), 500), Site: plain(first("og:site_name"), 100)}
	if p.Site == "" {
		p.Site = base.Hostname()
	}
	if p.Title == "" {
		p.Title = p.Site
	}
	image := ""
	if raw := first("og:image:secure_url", "og:image", "twitter:image", "twitter:image:src"); raw != "" {
		if relative, err := url.Parse(raw); err == nil {
			if safe, err := safeURL(base.ResolveReference(relative).String()); err == nil {
				image = safe.String()
			}
		}
	}
	return p, image, nil
}

var youtubeID = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

func youtubeVideo(u *url.URL) string {
	host := strings.ToLower(u.Hostname())
	var id string
	switch host {
	case "youtu.be":
		id = strings.Trim(u.Path, "/")
	case "youtube.com", "www.youtube.com", "m.youtube.com", "music.youtube.com":
		if u.Path == "/watch" {
			id = u.Query().Get("v")
		} else {
			parts := strings.Split(strings.Trim(u.Path, "/"), "/")
			if len(parts) == 2 && (parts[0] == "shorts" || parts[0] == "live" || parts[0] == "embed") {
				id = parts[1]
			}
		}
	}
	if youtubeID.MatchString(id) {
		return id
	}
	return ""
}
func metadata(ctx context.Context, client *http.Client, u *url.URL) (Preview, string, error) {
	if id := youtubeVideo(u); id != "" {
		canonical := "https://www.youtube.com/watch?v=" + id
		data, typ, _, err := fetch(ctx, client, "https://www.youtube.com/oembed?format=json&url="+url.QueryEscape(canonical), 64<<10)
		if err == nil && strings.Contains(typ, "json") {
			var value struct {
				Title     string `json:"title"`
				Thumbnail string `json:"thumbnail_url"`
			}
			if json.Unmarshal(data, &value) == nil && strings.TrimSpace(value.Title) != "" {
				image := ""
				if safe, err := safeURL(value.Thumbnail); err == nil {
					image = safe.String()
				}
				return Preview{URL: u.String(), Title: plain(value.Title, 300), Site: "YouTube"}, image, nil
			}
		}
	}
	data, typ, final, err := fetch(ctx, client, u.String(), 1<<20)
	if err != nil {
		return Preview{}, "", err
	}
	if !strings.Contains(typ, "text/html") && !strings.Contains(typ, "application/xhtml+xml") {
		return Preview{}, "", errUnavailable
	}
	preview, image, err := parseHTML(data, typ, final)
	// Keep the clicked URL visible even when the server redirects elsewhere.
	preview.URL = u.String()
	return preview, image, err
}
