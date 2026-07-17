// cmd/collect/main.go
// Run once to pull 100 manga entries from MangaDex API and append to data/manga.json
// Usage: go run cmd/collect/main.go
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// MangaDex API response shapes
type mdSearchResp struct {
	Data []struct {
		ID         string `json:"id"`
		Attributes struct {
			Title       map[string]string `json:"title"`
			Description map[string]string `json:"description"`
			Status      string            `json:"status"`
			LastChapter string            `json:"lastChapter"`
			Tags        []struct {
				Attributes struct {
					Name  map[string]string `json:"name"`
					Group string            `json:"group"`
				} `json:"attributes"`
			} `json:"tags"`
		} `json:"attributes"`
		Relationships []struct {
			Type       string `json:"type"`
			Attributes *struct {
				Name string `json:"name"`
			} `json:"attributes"`
		} `json:"relationships"`
	} `json:"data"`
}

type Manga struct {
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	Author        string   `json:"author"`
	Genres        []string `json:"genres"`
	Status        string   `json:"status"`
	TotalChapters int      `json:"total_chapters"`
	Description   string   `json:"description"`
	CoverURL      string   `json:"cover_url"`
}

func main() {
	outputPath := "data/manga_api.json"
	log.Println("Fetching manga from MangaDex API...")

	var allManga []Manga
	client := &http.Client{Timeout: 15 * time.Second}

	queries := []string{"action", "romance", "fantasy", "horror", "comedy", "sci-fi", "historical", "slice of life", "mystery", "sports"}

	for _, q := range queries {
		url := fmt.Sprintf(
			"https://api.mangadex.org/manga?title=%s&limit=10&contentRating[]=safe&contentRating[]=suggestive&includes[]=author&order[relevance]=desc",
			strings.ReplaceAll(q, " ", "+"),
		)
		resp, err := client.Get(url)
		if err != nil {
			log.Printf("Query %q failed: %v", q, err)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var parsed mdSearchResp
		if err := json.Unmarshal(body, &parsed); err != nil {
			log.Printf("Parse error for %q: %v", q, err)
			continue
		}

		for _, item := range parsed.Data {
			title := item.Attributes.Title["en"]
			if title == "" {
				for _, v := range item.Attributes.Title {
					title = v
					break
				}
			}
			if title == "" {
				continue
			}

			// Extract author
			author := "Unknown"
			for _, rel := range item.Relationships {
				if rel.Type == "author" && rel.Attributes != nil {
					author = rel.Attributes.Name
					break
				}
			}

			// Extract genres from tags
			var genres []string
			for _, tag := range item.Attributes.Tags {
				if tag.Attributes.Group == "genre" {
					if name, ok := tag.Attributes.Name["en"]; ok {
						genres = append(genres, name)
					}
				}
			}
			if len(genres) == 0 {
				genres = []string{"Action"}
			}

			// Description
			desc := item.Attributes.Description["en"]
			if len(desc) > 200 {
				desc = desc[:200] + "..."
			}

			// ID slug from UUID
			slug := "md-" + item.ID[:8]

			chapters := 0
			fmt.Sscanf(item.Attributes.LastChapter, "%d", &chapters)

			status := item.Attributes.Status
			if status == "" {
				status = "ongoing"
			}

			allManga = append(allManga, Manga{
				ID:            slug,
				Title:         title,
				Author:        author,
				Genres:        genres,
				Status:        status,
				TotalChapters: chapters,
				Description:   desc,
				CoverURL:      fmt.Sprintf("https://uploads.mangadex.org/covers/%s/", item.ID),
			})
		}

		log.Printf("[%s] fetched %d manga (running total: %d)", q, len(parsed.Data), len(allManga))
		time.Sleep(500 * time.Millisecond) // rate limiting
	}

	// Deduplicate by title
	seen := map[string]bool{}
	var unique []Manga
	for _, m := range allManga {
		if !seen[m.Title] {
			seen[m.Title] = true
			unique = append(unique, m)
		}
	}

	data, err := json.MarshalIndent(unique, "", "  ")
	if err != nil {
		log.Fatal("Marshal error:", err)
	}
	if err := os.WriteFile(outputPath, data, 0644); err != nil {
		log.Fatal("Write error:", err)
	}
	log.Printf("Saved %d unique manga to %s", len(unique), outputPath)
}
