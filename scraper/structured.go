package scraper

import (
	"encoding/json"
	"fmt"
	"strings"

	"golang.org/x/net/html"
)

// StructuredRecord is normalized schema.org-style data.
type StructuredRecord struct {
	Type    string
	Name    string
	Price   string
	Rating  string
	Address string
	RawJSON string
	Source  string
}

// ExtractJSONLD parses application/ld+json blocks from HTML (spec ss28.12b.8).
func ExtractJSONLD(htmlDoc string) ([]StructuredRecord, error) {
	doc, err := html.Parse(strings.NewReader(htmlDoc))
	if err != nil {
		return nil, fmt.Errorf("structured: parse html: %w", err)
	}
	var out []StructuredRecord
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "script" {
			var typ string
			for _, a := range n.Attr {
				if a.Key == "type" && strings.EqualFold(a.Val, "application/ld+json") {
					typ = a.Val
					break
				}
			}
			if typ != "" && n.FirstChild != nil {
				raw := strings.TrimSpace(n.FirstChild.Data)
				rec, err := parseJSONLDBlock(raw)
				if err == nil {
					out = append(out, rec)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return out, nil
}

func parseJSONLDBlock(raw string) (StructuredRecord, error) {
	var generic map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &generic); err != nil {
		return StructuredRecord{}, err
	}
	rec := StructuredRecord{RawJSON: raw, Source: "json-ld"}
	if t, ok := generic["@type"]; ok {
		var typeStr string
		_ = json.Unmarshal(t, &typeStr)
		rec.Type = typeStr
	}
	if name, ok := generic["name"]; ok {
		_ = json.Unmarshal(name, &rec.Name)
	}
	if offers, ok := generic["offers"]; ok {
		var offer struct {
			Price string `json:"price"`
		}
		_ = json.Unmarshal(offers, &offer)
		rec.Price = offer.Price
	}
	if rating, ok := generic["aggregateRating"]; ok {
		var agg struct {
			RatingValue string `json:"ratingValue"`
		}
		_ = json.Unmarshal(rating, &agg)
		rec.Rating = agg.RatingValue
	}
	if addr, ok := generic["address"]; ok {
		var a struct {
			StreetAddress string `json:"streetAddress"`
		}
		_ = json.Unmarshal(addr, &a)
		rec.Address = a.StreetAddress
	}
	return rec, nil
}

// ExtractOpenGraph reads og:* meta tags.
func ExtractOpenGraph(htmlDoc string) map[string]string {
	doc, err := html.Parse(strings.NewReader(htmlDoc))
	if err != nil {
		return nil
	}
	out := make(map[string]string)
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "meta" {
			var prop, content string
			for _, a := range n.Attr {
				switch a.Key {
				case "property":
					prop = a.Val
				case "content":
					content = a.Val
				}
			}
			if strings.HasPrefix(prop, "og:") && content != "" {
				out[prop] = content
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return out
}
