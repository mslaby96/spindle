package discogs

import (
	"context"
	"net/url"
	"strconv"
)

// collectionPages caps how far a collection is read: 100 pages of 100 is far more than any collection a person has.
var collectionPages = 100

// CollectionItem is one record in a user's Discogs collection, as much as the collection listing tells about it.
type CollectionItem struct {
	ReleaseID int64
	// MasterID is 0 for a release that isn't part of a master.
	MasterID int64
	Title    string
	Artist   string
	// Year is the pressing's, not the album's original year.
	Year     int
	Genres   []string
	Styles   []string
	Label    string
	CatNo    string
	Format   string
	CoverURL string
}

// Identity returns the username of the account the token belongs to.
func (c *Client) Identity(ctx context.Context) (string, error) {
	var out struct {
		Username string `json:"username"`
	}
	err := c.get(ctx, "/oauth/identity", nil, &out)
	return out.Username, err
}

type collectionPage struct {
	Pagination struct {
		Pages int `json:"pages"`
	} `json:"pagination"`
	Releases []struct {
		Basic struct {
			ID         int64    `json:"id"`
			MasterID   int64    `json:"master_id"`
			Title      string   `json:"title"`
			Year       int      `json:"year"`
			CoverImage string   `json:"cover_image"`
			Formats    []Format `json:"formats"`
			Labels     []Label  `json:"labels"`
			Artists    []Artist `json:"artists"`
			Genres     []string `json:"genres"`
			Styles     []string `json:"styles"`
		} `json:"basic_information"`
	} `json:"releases"`
}

// Collection reads every record in the user's collection, all folders together.
func (c *Client) Collection(ctx context.Context, username string) ([]CollectionItem, error) {
	var items []CollectionItem
	for page := 1; page <= collectionPages; page++ {
		var p collectionPage
		q := url.Values{"page": {strconv.Itoa(page)}, "per_page": {"100"}}
		if err := c.get(ctx, "/users/"+url.PathEscape(username)+"/collection/folders/0/releases", q, &p); err != nil {
			return nil, err
		}
		for _, r := range p.Releases {
			b := r.Basic
			item := CollectionItem{
				ReleaseID: b.ID, MasterID: b.MasterID, Title: CleanName(b.Title), Artist: ArtistNames(b.Artists),
				Year: b.Year, Genres: b.Genres, Styles: b.Styles, CoverURL: b.CoverImage,
				Format: Release{Formats: b.Formats}.FormatText(),
			}
			if len(b.Labels) > 0 {
				item.Label, item.CatNo = CleanName(b.Labels[0].Name), b.Labels[0].CatNo
			}
			items = append(items, item)
		}
		if page >= p.Pagination.Pages {
			break
		}
	}
	return items, nil
}
