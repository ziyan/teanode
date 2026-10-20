package linkpreview_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/linkpreview"
)

func TestParse(t *testing.T) {
	base, _ := url.Parse("https://shop.example.com/items/7/")
	cases := []struct {
		name string
		html string
		want linkpreview.Page
	}{
		{
			name: "open graph tags",
			html: `<html><head><title>Plain title</title>
				<meta property="og:title" content="Garden Chair">
				<meta property="og:description" content="A chair for the garden.">
				<meta property="og:site_name" content="Example Shop">
				<meta property="og:image" content="https://cdn.example.com/chair.jpg">
				</head></html>`,
			want: linkpreview.Page{Title: "Garden Chair", Description: "A chair for the garden.", SiteName: "Example Shop", ImageURL: "https://cdn.example.com/chair.jpg"},
		},
		{
			name: "twitter tags when there are no open graph tags",
			html: `<head><meta name="twitter:title" content="Short title"><meta name="twitter:description" content="Short line"><meta name="twitter:image" content="/media/a.png"></head>`,
			want: linkpreview.Page{Title: "Short title", Description: "Short line", ImageURL: "https://shop.example.com/media/a.png"},
		},
		{
			name: "title and description of the page itself",
			html: `<head><title> The   page
				title </title><meta name="description" content="What the page says"></head>`,
			want: linkpreview.Page{Title: "The page title", Description: "What the page says"},
		},
		{
			name: "open graph wins over twitter and the page",
			html: `<head><title>Page</title><meta name="twitter:title" content="Twitter"><meta property="og:title" content="Graph"></head>`,
			want: linkpreview.Page{Title: "Graph"},
		},
		{
			name: "relative picture address",
			html: `<head><meta property="og:image" content="../cover.png"></head>`,
			want: linkpreview.Page{ImageURL: "https://shop.example.com/items/cover.png"},
		},
		{
			name: "the first of a repeated picture wins",
			html: `<head><meta property="og:image" content="https://cdn.example.com/one.png"><meta property="og:image" content="https://cdn.example.com/two.png"></head>`,
			want: linkpreview.Page{ImageURL: "https://cdn.example.com/one.png"},
		},
		{
			name: "a picture that is not on the web is dropped",
			html: `<head><title>T</title><meta property="og:image" content="data:image/png;base64,AAAA"></head>`,
			want: linkpreview.Page{Title: "T"},
		},
		{
			name: "entities are decoded and white space is tidied",
			html: "<head><meta property=\"og:title\" content=\"Tom &amp; Jerry&#39;s\n  &quot;Big&quot; Day\"></head>",
			want: linkpreview.Page{Title: `Tom & Jerry's "Big" Day`},
		},
	}
	for _, found := range cases {
		t.Run(found.name, func(t *testing.T) {
			page, err := linkpreview.Parse(strings.NewReader(found.html), base)
			if err != nil {
				t.Fatalf("Parse: %s", err)
			}
			if *page != found.want {
				t.Errorf("Parse = %+v, want %+v", *page, found.want)
			}
		})
	}
}

func TestParseCutsALongTitleAndDescription(t *testing.T) {
	html := `<head><meta property="og:title" content="` + strings.Repeat("a", 300) + `"><meta property="og:description" content="` + strings.Repeat("b", 900) + `"></head>`
	page, err := linkpreview.Parse(strings.NewReader(html), nil)
	if err != nil {
		t.Fatalf("Parse: %s", err)
	}
	if got := len([]rune(page.Title)); got != 200 {
		t.Errorf("the title has %d characters, want 200", got)
	}
	if got := len([]rune(page.Description)); got != 500 {
		t.Errorf("the description has %d characters, want 500", got)
	}
	if !strings.HasSuffix(page.Title, "…") {
		t.Errorf("the cut title does not end in an ellipsis: %q", page.Title[len(page.Title)-4:])
	}
}

func TestHasWords(t *testing.T) {
	if (&linkpreview.Page{ImageURL: "https://example.com/a.png", SiteName: "Example"}).HasWords() {
		t.Error("a page with only a picture and a site name has words")
	}
	if !(&linkpreview.Page{Description: "x"}).HasWords() {
		t.Error("a page with a description has no words")
	}
}
